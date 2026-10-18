package metadata

import (
	"bytes"
	"context"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

func TestReplicatedCheckpointPreservesSecurityStateAfterPrimaryLoss(t *testing.T) {
	ctx := context.Background()
	primary := filepath.Join(t.TempDir(), "primary.sqlite")
	store, err := Open(ctx, primary)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	credential, token, err := store.CreateAccessKey(ctx, "synthetic checkpoint user")
	if err != nil {
		t.Fatal(err)
	}
	m := fixture(t)
	if err = store.Save(ctx, m); err != nil {
		t.Fatal(err)
	}
	if err = store.GrantFileAccess(ctx, credential.PrincipalID, m.FileID, "encrypted.tankenc"); err != nil {
		t.Fatal(err)
	}
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	account := strings.ToLower(crypto.PubkeyToAddress(key.PublicKey).Hex())
	contract := common.HexToAddress("0x1111111111111111111111111111111111111111")
	target := "5042002:" + strings.ToLower(contract.Hex()) + ":" + account
	if err = store.EnqueueRegistration(ctx, m.FileID, target); err != nil {
		t.Fatal(err)
	}
	job, err := store.ClaimRegistration(ctx, target, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	tx := types.NewTx(&types.DynamicFeeTx{ChainID: big.NewInt(5042002), To: &contract, Gas: 21000, GasFeeCap: big.NewInt(20000000000), GasTipCap: big.NewInt(0)})
	signed, err := types.SignTx(tx, types.LatestSignerForChainID(tx.ChainId()), key)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := signed.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	prepared := PreparedRegistration{ChainID: 5042002, Account: account, FileID: m.FileID, Target: target, Hash: signed.Hash().Hex(), Raw: raw}
	if err = store.SavePreparedRegistration(ctx, job, prepared); err != nil {
		t.Fatal(err)
	}
	budget := big.NewInt(420000000000000)
	if err = store.ReserveRegistrationSpend(ctx, job, prepared, budget); err != nil {
		t.Fatal(err)
	}
	if err = store.RevokeAccessKey(ctx, credential.ID); err != nil {
		t.Fatal(err)
	}
	before, err := store.RestoreSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	copies := []string{filepath.Join(t.TempDir(), "copy-a"), filepath.Join(t.TempDir(), "copy-b")}
	completed, err := CreateCheckpoints(ctx, primary, copies)
	if err != nil || len(completed) != 2 {
		t.Fatalf("checkpoint: %v, %d", err, len(completed))
	}
	a, _ := VerifyCheckpoint(ctx, copies[0], "")
	if _, err = VerifyCheckpoint(ctx, copies[1], a.SHA256); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(primary); err != nil {
		t.Fatal(err)
	} // Only synthetic test primary.
	for _, path := range copies {
		restored, err := Open(ctx, filepath.Join(path, "snapshot.sqlite"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err = restored.AuthenticateAccessKey(ctx, token); !errors.Is(err, ErrUnauthorized) {
			t.Fatal("revocation lost", err)
		}
		value, err := restored.PreparedRegistration(ctx, job, 5042002, account)
		if err != nil || !bytes.Equal(value.Raw, raw) || value.Hash != prepared.Hash {
			t.Fatal("signed bytes lost", err)
		}
		after, err := restored.RestoreSnapshot(ctx)
		if err != nil || len(after.Reservations) != 1 || after.Accounts[0] != before.Accounts[0] || after.Reservations[0] != before.Reservations[0] {
			t.Fatal("reservation/budget lost", err)
		}
		if _, err = restored.LoadFileAccess(ctx, credential.PrincipalID, m.FileID); err != nil {
			t.Fatal("ownership lost", err)
		}
		if _, err = restored.LoadFileAccess(ctx, "another-user", m.FileID); !errors.Is(err, ErrAccessDenied) {
			t.Fatal("cross-user access", err)
		}
		restored.Close()
	}
}

func TestCheckpointRejectsDamagePartialAliasesAndWrongGeneration(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	primary := filepath.Join(dir, "primary.sqlite")
	store, err := Open(ctx, primary)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	key, _, err := store.CreateAccessKey(ctx, "synthetic user")
	if err != nil {
		t.Fatal(err)
	}
	paths := []string{filepath.Join(dir, "a"), filepath.Join(dir, "b")}
	if _, err = CreateCheckpoints(ctx, primary, paths); err != nil {
		t.Fatal(err)
	}
	old, err := VerifyCheckpoint(ctx, paths[0], "")
	if err != nil {
		t.Fatal(err)
	}
	if err = store.RevokeAccessKey(ctx, key.ID); err != nil {
		t.Fatal(err)
	}
	newer := []string{filepath.Join(dir, "c"), filepath.Join(dir, "d")}
	if _, err = CreateCheckpoints(ctx, primary, newer); err != nil {
		t.Fatal(err)
	}
	current, err := VerifyCheckpoint(ctx, newer[0], "")
	if err != nil {
		t.Fatal(err)
	}
	if current.SHA256 == old.SHA256 {
		t.Fatal("mutation absent from snapshot")
	}
	if _, err = VerifyCheckpoint(ctx, paths[0], current.SHA256); err == nil {
		t.Fatal("stale generation accepted against pinned newest digest")
	}
	if _, err = CreateCheckpoints(ctx, primary, paths); err == nil {
		t.Fatal("overwrote bundles")
	}
	duplicate := filepath.Join(dir, "duplicate")
	if _, err = CreateCheckpoints(ctx, primary, []string{duplicate, filepath.Join(dir, ".", "duplicate")}); err == nil {
		t.Fatal("alias accepted")
	}
	if err = os.Symlink(dir, filepath.Join(dir, "alias")); err != nil {
		t.Fatal(err)
	}
	if _, err = CreateCheckpoints(ctx, primary, []string{filepath.Join(dir, "alias", "new"), filepath.Join(dir, "other")}); err == nil {
		t.Fatal("symlink parent accepted")
	}
	partial := filepath.Join(dir, "partial")
	if err = os.Mkdir(partial, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err = VerifyCheckpoint(ctx, partial, ""); err == nil {
		t.Fatal("partial publication accepted")
	}
	if err = os.WriteFile(filepath.Join(paths[0], "snapshot.sqlite"), []byte("damaged"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = VerifyCheckpoint(ctx, paths[0], ""); err == nil {
		t.Fatal("corruption accepted")
	}
	if _, err = VerifyCheckpoint(ctx, paths[1], old.SHA256); err != nil {
		t.Fatal("independent retained copy damaged", err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err = CreateCheckpoints(canceled, primary, []string{filepath.Join(dir, "cancel-a"), filepath.Join(dir, "cancel-b")}); err == nil {
		t.Fatal("cancellation ignored")
	}
	if _, err = VerifyCheckpoint(canceled, paths[1], ""); err == nil {
		t.Fatal("verify cancellation ignored")
	}
}
