package metadata

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"tank.local/tank/internal/encoding"
	"tank.local/tank/internal/integrity"
)

func fixture(t *testing.T) Manifest {
	t.Helper()

	data := []byte("Tank persistence test")
	shards, err := encoding.Encode(data)
	if err != nil {
		t.Fatal(err)
	}

	root, _, err := integrity.BuildMerkleRoot(shards)
	if err != nil {
		t.Fatal(err)
	}

	records := make([]Shard, len(shards))
	for i, shard := range shards {
		records[i] = Shard{
			Index: i,
			Hash:  integrity.Digest(shard).String(),
		}
	}

	return Manifest{
		Version:   1,
		FileID:    integrity.Digest(data).String(),
		Size:      int64(len(data)),
		CreatedAt: time.Now().UTC(),
		Segments: []Segment{{
			Index:      0,
			Size:       len(data),
			MerkleRoot: root.String(),
			Shards:     records,
		}},
	}
}

func TestPersistenceAndDuplicateProtection(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "tank.sqlite")

	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}

	m := fixture(t)

	if err := store.Save(ctx, m); err != nil {
		store.Close()
		t.Fatal(err)
	}

	if err := store.Save(ctx, m); !errors.Is(err, ErrExists) {
		store.Close()
		t.Fatalf("expected ErrExists, got %v", err)
	}

	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()

	got, err := reopened.Load(ctx, m.FileID)
	if err != nil {
		t.Fatal(err)
	}
	if got.FileID != m.FileID ||
		got.Size != m.Size ||
		got.Segments[0].MerkleRoot != m.Segments[0].MerkleRoot ||
		!got.CreatedAt.Equal(m.CreatedAt) {
		t.Fatal("manifest changed after reopening")
	}

	ids, err := reopened.List(ctx, "", 10)
	if err != nil || len(ids) != 1 || ids[0] != m.FileID {
		t.Fatalf("unexpected list: %v, error: %v", ids, err)
	}

	next, err := reopened.List(ctx, ids[0], 10)
	if err != nil || len(next) != 0 {
		t.Fatal("pagination repeated a file")
	}

	missing := integrity.Digest([]byte("missing")).String()
	if _, err := reopened.Load(ctx, missing); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestInvalidManifestRejected(t *testing.T) {
	m := fixture(t)
	m.Size++

	if err := m.Validate(); err == nil {
		t.Fatal("accepted inconsistent file size")
	}

	m = fixture(t)
	m.Segments[0].Shards[0].Index = 5

	if err := m.Validate(); err == nil {
		t.Fatal("accepted incorrect shard index")
	}
}
