package metadata

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenExistingDoesNotBootstrap(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "missing", "authority.sqlite")
	if store, err := OpenExisting(ctx, path); err == nil {
		store.Close()
		t.Fatal("missing authority accepted")
	}
	if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
		t.Fatalf("created directory: %v", err)
	}
	path = filepath.Join(t.TempDir(), "empty.sqlite")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if store, err := OpenExisting(ctx, path); err == nil {
		store.Close()
		t.Fatal("empty authority accepted")
	}
	info, err := os.Stat(path)
	if err != nil || info.Size() != 0 {
		t.Fatal("modified uninitialized authority")
	}
}

func TestOpenExistingPreservesAuthorityRecords(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "authority.sqlite")
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	key, token, err := store.CreateAccessKey(ctx, "Recovery test")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RevokeAccessKey(ctx, key.ID); err != nil {
		t.Fatal(err)
	}
	store.Close()
	store, err = OpenExisting(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.AuthenticateAccessKey(ctx, token); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("revocation lost: %v", err)
	}
}
