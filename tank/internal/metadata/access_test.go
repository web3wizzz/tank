package metadata

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestAccessIsolationPersistenceAndRevocation(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "tank.sqlite")
	store, err := Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}

	aliceKey, aliceToken, err := store.CreateAccessKey(ctx, "Alice")
	if err != nil {
		t.Fatal(err)
	}
	bobKey, bobToken, err := store.CreateAccessKey(ctx, "Bob")
	if err != nil {
		t.Fatal(err)
	}
	if aliceToken == bobToken {
		t.Fatal("users received identical credentials")
	}

	m := filenameTestManifest(t)
	if err := store.Save(ctx, m); err != nil {
		t.Fatal(err)
	}
	if err := store.GrantFileAccess(ctx, aliceKey.PrincipalID, m.FileID, "alice.pdf"); err != nil {
		t.Fatal(err)
	}

	if _, err := store.LoadFileAccess(ctx, bobKey.PrincipalID, m.FileID); !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("Bob accessed Alice's file: %v", err)
	}
	ids, err := store.ListAccessibleFiles(ctx, bobKey.PrincipalID, "")
	if err != nil || len(ids) != 0 {
		t.Fatalf("Bob's file list: %v, %v", ids, err)
	}
	if err := store.GrantFileAccess(ctx, bobKey.PrincipalID, strings.Repeat("0", 64), "missing.pdf"); !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("granted nonexistent file: %v", err)
	}

	var storedHash string
	if err := store.db.QueryRowContext(ctx,
		"SELECT token_hash FROM api_credentials WHERE id = ?", aliceKey.ID,
	).Scan(&storedHash); err != nil {
		t.Fatal(err)
	}
	if storedHash == aliceToken || len(storedHash) != 64 {
		t.Fatal("credential was not stored as a hash")
	}

	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	principal, err := store.AuthenticateAccessKey(ctx, aliceToken)
	if err != nil || principal.ID != aliceKey.PrincipalID {
		t.Fatalf("credential persistence: %v", err)
	}
	access, err := store.LoadFileAccess(ctx, principal.ID, m.FileID)
	if err != nil || access.Filename != "alice.pdf" {
		t.Fatalf("file permission persistence: %+v, %v", access, err)
	}
	ids, err = store.ListAccessibleFiles(ctx, principal.ID, "")
	if err != nil || len(ids) != 1 || ids[0] != m.FileID {
		t.Fatalf("Alice's file list: %v, %v", ids, err)
	}

	if _, err := store.db.ExecContext(ctx,
		"UPDATE api_credentials SET expires_at = 1 WHERE id = ?", bobKey.ID,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AuthenticateAccessKey(ctx, bobToken); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("expired credential accepted: %v", err)
	}

	if err := store.RevokeAccessKey(ctx, aliceKey.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AuthenticateAccessKey(ctx, aliceToken); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("revoked credential accepted: %v", err)
	}
	if _, err := store.AuthenticateAccessKey(ctx, "invalid"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("invalid credential accepted: %v", err)
	}
}
