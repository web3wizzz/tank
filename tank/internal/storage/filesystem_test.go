package storage

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"tank.local/tank/internal/integrity"
)

func TestFilesystemPersistence(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()

	store, err := NewFilesystem(directory)
	if err != nil {
		t.Fatal(err)
	}

	data := []byte("Tank shard bytes")
	key := ShardKey{
		FileID:  integrity.Digest([]byte("original file")).String(),
		Segment: 0,
		Index:   0,
	}

	if err := store.Put(ctx, key, data, integrity.Digest(data)); err != nil {
		store.Close()
		t.Fatal(err)
	}

	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := NewFilesystem(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()

	got, err := reopened.Get(ctx, key)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("reopened shard mismatch: %v", err)
	}

	// A rejected write must preserve the existing shard.
	err = reopened.Put(ctx, key, []byte("wrong bytes"), integrity.Digest(data))
	if err == nil {
		t.Fatal("accepted wrong hash")
	}

	got, err = reopened.Get(ctx, key)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatal("rejected write changed the existing shard")
	}

	if err := reopened.Delete(ctx, key); err != nil {
		t.Fatal(err)
	}
	if err := reopened.Delete(ctx, key); err != nil {
		t.Fatal("repeated deletion should succeed")
	}
	if _, err := reopened.Get(ctx, key); !errors.Is(err, ErrShardNotFound) {
		t.Fatalf("expected ErrShardNotFound, got %v", err)
	}
}

func TestFilesystemRejectsInvalidKeys(t *testing.T) {
	store, err := NewFilesystem(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	id := integrity.Digest([]byte("file")).String()
	keys := []ShardKey{
		{FileID: "../../outside", Segment: 0, Index: 0},
		{FileID: id, Segment: -1, Index: 0},
		{FileID: id, Segment: 0, Index: 6},
	}

	for _, key := range keys {
		if _, err := store.Get(context.Background(), key); err == nil {
			t.Fatalf("accepted invalid key: %+v", key)
		}
	}
}

func TestFilesystemRejectsSymlink(t *testing.T) {
	directory := t.TempDir()
	store, err := NewFilesystem(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}

	key := ShardKey{
		FileID: integrity.Digest([]byte("file")).String(),
	}
	name, err := shardFilename(key)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.Symlink(outside, filepath.Join(directory, name)); err != nil {
		t.Fatal(err)
	}

	if _, err := store.Get(context.Background(), key); err == nil {
		t.Fatal("accepted symlink as shard")
	}
}
