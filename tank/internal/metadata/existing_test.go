package metadata

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
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

func TestWritableOpensRejectRestoreMarkersAndDatabaseAliases(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "candidate.sqlite")
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	credential, token, err := store.CreateAccessKey(ctx, "synthetic gated user")
	if err != nil {
		t.Fatal(err)
	}
	if err = store.RevokeAccessKey(ctx, credential.ID); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path+".restore-gate", []byte("any marker blocks"), 0600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(dir, "alias.sqlite")
	if err = os.Symlink(filepath.Base(path), alias); err != nil {
		t.Fatal(err)
	}
	for _, candidate := range []string{path, alias} {
		for _, open := range []func(context.Context, string) (*Store, error){Open, OpenExisting} {
			opened, err := open(ctx, candidate)
			if opened != nil {
				opened.Close()
			}
			if !errors.Is(err, ErrRestoreBlocked) {
				t.Fatalf("gated writable open accepted: %v", err)
			}
		}
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("rejected opens changed database", err)
	}
	readOnly, err := OpenRestoreReadOnly(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer readOnly.Close()
	if _, err = readOnly.AuthenticateAccessKey(ctx, token); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("read-only inspection lost revocation", err)
	}
	copy := filepath.Join(dir, "inspection-copy.sqlite")
	if err = BackupDatabase(ctx, path, copy); err != nil {
		t.Fatal("read-only backup refused gated source", err)
	}
}

func TestWritableOpenRejectsGatedBootstrapIncludingDanglingAlias(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "not-created.sqlite")
	// Even an invalid or symlink marker is a block, never permission to bootstrap.
	if err := os.Symlink(filepath.Join(dir, "absent-marker-target"), path+".restore-gate"); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(dir, "dangling-alias.sqlite")
	if err := os.Symlink(path, alias); err != nil {
		t.Fatal(err)
	}
	for _, candidate := range []string{path, alias} {
		for _, open := range []func(context.Context, string) (*Store, error){Open, OpenExisting} {
			store, err := open(ctx, candidate)
			if store != nil {
				store.Close()
			}
			if !errors.Is(err, ErrRestoreBlocked) {
				t.Fatalf("gated bootstrap accepted: %v", err)
			}
		}
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("gated bootstrap created database", err)
	}
}

func TestWritableOpenChecksIntermediateAliasMarker(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "uncreated.sqlite")
	intermediate := filepath.Join(dir, "intermediate.sqlite")
	alias := filepath.Join(dir, "outer.sqlite")
	if err := os.Symlink(target, intermediate); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(intermediate+".restore-gate", []byte("inactive alias"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(intermediate, alias); err != nil {
		t.Fatal(err)
	}
	for _, open := range []func(context.Context, string) (*Store, error){Open, OpenExisting} {
		store, err := open(context.Background(), alias)
		if store != nil {
			store.Close()
		}
		if !errors.Is(err, ErrRestoreBlocked) {
			t.Fatal("intermediate alias marker bypassed", err)
		}
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("blocked alias created target", err)
	}
}

func TestWritableOpenRefusesHardLinksAndNonregularFiles(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "candidate.sqlite")
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	store.Close()
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path+".restore-gate", []byte("inactive"), 0600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(dir, "hardlink.sqlite")
	if err = os.Link(path, alias); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(dir, "not-a-database")
	if err = syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	for _, candidate := range []string{path, alias, fifo, dir} {
		for _, open := range []func(context.Context, string) (*Store, error){Open, OpenExisting} {
			opened, err := open(ctx, candidate)
			if opened != nil {
				opened.Close()
			}
			if !errors.Is(err, ErrRestoreBlocked) {
				t.Fatalf("unsafe file accepted: %v", err)
			}
		}
	}
	after, err := os.ReadFile(alias)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("hard-linked database modified", err)
	}
	info, err := os.Lstat(alias)
	if err != nil || info.Sys().(*syscall.Stat_t).Nlink != 2 {
		t.Fatal("hard link altered", err)
	}
	if _, err = os.Lstat(path + ".restore-gate"); err != nil {
		t.Fatal("marker altered", err)
	}
}
