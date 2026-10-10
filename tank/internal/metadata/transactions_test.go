package metadata

import (
	"context"
	"errors"
	"github.com/ethereum/go-ethereum/crypto"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestJournalMigrationPreservesSchemaSixAndStaleFences(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "old.sqlite")
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { store.Close() }()
	key, token, err := store.CreateAccessKey(ctx, "migration fixture")
	if err != nil {
		t.Fatal(err)
	}
	manifest := fixture(t)
	if err := store.Save(ctx, manifest); err != nil {
		t.Fatal(err)
	}
	if err := store.GrantFileAccess(ctx, key.PrincipalID, manifest.FileID, "encrypted.tankenc"); err != nil {
		t.Fatal(err)
	}
	account := "0x" + strings.Repeat("a", 40)
	target := "84532:0x" + strings.Repeat("b", 40) + ":" + account
	if err := store.EnqueueRegistration(ctx, manifest.FileID, target); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, "DROP TABLE registration_transactions; PRAGMA user_version=6"); err != nil {
		t.Fatal(err)
	}
	store.Close()
	store, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AuthenticateAccessKey(ctx, token); err != nil {
		t.Fatal("migration lost authentication")
	}
	if access, err := store.LoadFileAccess(ctx, key.PrincipalID, manifest.FileID); err != nil || access.Filename != "encrypted.tankenc" {
		t.Fatal("migration lost file authorization")
	}
	job, err := store.ClaimRegistration(ctx, target, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte("opaque signed fixture bytes; cryptographic validation belongs to registry")
	prepared := PreparedRegistration{ChainID: 84532, Account: account, FileID: job.FileID, Target: target, Nonce: 0, Hash: strings.ToLower(crypto.Keccak256Hash(raw).Hex()), Raw: raw}
	if err := store.SavePreparedRegistration(ctx, job, prepared); err != nil {
		t.Fatal(err)
	}
	// An abandoned process lease must not make its account lane reusable.
	if _, err := store.db.ExecContext(ctx, "UPDATE registration_jobs SET lease_until=0 WHERE file_id=? AND target=?", job.FileID, job.Target); err != nil {
		t.Fatal(err)
	}
	if err := store.SavePreparedRegistration(ctx, job, prepared); !errors.Is(err, ErrLeaseLost) {
		t.Fatal("stale job rewrote journal")
	}
	current, err := store.ClaimRegistration(ctx, target, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CompletePreparedRegistration(ctx, job, prepared); !errors.Is(err, ErrLeaseLost) {
		t.Fatal("stale job confirmed account lane")
	}
	restored, err := store.PreparedRegistration(ctx, current, 84532, account)
	if err != nil || string(restored.Raw) != string(raw) {
		t.Fatal("lease expiry discarded prepared bytes")
	}
	snapshot := filepath.Join(t.TempDir(), "journal-backup.sqlite")
	if err := store.Backup(ctx, snapshot); err != nil {
		t.Fatal(err)
	}
	backup, err := Open(ctx, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	defer backup.Close()
	restored, err = backup.PreparedRegistration(ctx, current, 84532, account)
	if err != nil || restored.Hash != prepared.Hash || string(restored.Raw) != string(raw) {
		t.Fatal("private backup lost outstanding signer lane")
	}
}
