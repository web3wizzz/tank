package metadata

import (
	"context"
	"errors"
	"math/big"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

func TestRegistrationSpendBudgetSurvivesRetryRestartAndConcurrentReservations(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "journal.sqlite")
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { store.Close() }()
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	account := strings.ToLower(crypto.PubkeyToAddress(key.PublicKey).Hex())
	contract := common.HexToAddress("0x1111111111111111111111111111111111111111")
	target := "5042002:" + strings.ToLower(contract.Hex()) + ":" + account
	m := fixture(t)
	if err := store.Save(ctx, m); err != nil {
		t.Fatal(err)
	}
	if err := store.EnqueueRegistration(ctx, m.FileID, target); err != nil {
		t.Fatal(err)
	}
	job, err := store.ClaimRegistration(ctx, target, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	prepare := func(nonce uint64) PreparedRegistration {
		tx := types.NewTx(&types.DynamicFeeTx{ChainID: big.NewInt(5042002), Nonce: nonce, To: &contract, Gas: 21000, GasFeeCap: big.NewInt(20000000000), GasTipCap: big.NewInt(0)})
		signed, err := types.SignTx(tx, types.LatestSignerForChainID(tx.ChainId()), key)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := signed.MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}
		value := PreparedRegistration{ChainID: 5042002, Account: account, FileID: m.FileID, Target: target, Nonce: nonce, Hash: signed.Hash().Hex(), Raw: raw}
		if err := store.SavePreparedRegistration(ctx, job, value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	value := prepare(0)
	budget := big.NewInt(420000000000000) // Exactly one transaction ceiling.
	second, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, writer := range []*Store{store, second} {
		wg.Add(1)
		go func(writer *Store) { defer wg.Done(); errs <- writer.ReserveRegistrationSpend(ctx, job, value, budget) }(writer)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	second.Close()
	store.Close()
	store, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ReserveRegistrationSpend(ctx, job, value, budget); err != nil {
		t.Fatal("restart charged a retry again", err)
	}
	if err := store.ReserveRegistrationSpend(ctx, job, value, new(big.Int).Mul(budget, big.NewInt(2))); !errors.Is(err, ErrSpendBudget) {
		t.Fatal("budget silently increased")
	}
	if err := store.CompletePreparedRegistration(ctx, job, value); err != nil {
		t.Fatal(err)
	}
	// Reset the fixture job only, retaining the confirmed lane and spend ledger.
	if _, err := store.db.ExecContext(ctx, `UPDATE registration_jobs SET status='pending',tx_hash='',next_attempt=0 WHERE file_id=? AND target=?`, m.FileID, target); err != nil {
		t.Fatal(err)
	}
	job, err = store.ClaimRegistration(ctx, target, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	value = prepare(1)
	if err := store.ReserveRegistrationSpend(ctx, job, value, budget); !errors.Is(err, ErrSpendBudget) {
		t.Fatal("confirmed reservation was lost")
	}
}
