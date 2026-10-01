package metadata

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"tank.local/tank/internal/integrity"
)

func cloneManifest(t *testing.T, m Manifest) Manifest {
	t.Helper()

	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}

	var result Manifest
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}

	return result
}

func TestReplaceLocations(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "tank.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	original := fixture(t)
	for i := range original.Segments {
		for j := range original.Segments[i].Shards {
			original.Segments[i].Shards[j].NodeURL = "http://127.0.0.1:9101"
		}
	}

	if err := store.Save(ctx, original); err != nil {
		t.Fatal(err)
	}

	updated := cloneManifest(t, original)
	updated.Segments[0].Shards[0].NodeURL = "http://127.0.0.1:9104"

	if err := store.ReplaceLocations(ctx, original, updated); err != nil {
		t.Fatal(err)
	}

	got, err := store.Load(ctx, original.FileID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Segments[0].Shards[0].NodeURL != "http://127.0.0.1:9104" {
		t.Fatal("replacement location was not saved")
	}

	// A stale repair must not overwrite the saved update.
	stale := cloneManifest(t, original)
	stale.Segments[0].Shards[0].NodeURL = "http://127.0.0.1:9105"

	err = store.ReplaceLocations(ctx, original, stale)
	if !errors.Is(err, ErrManifestConflict) {
		t.Fatalf("expected conflict, got %v", err)
	}

	// Repair must not change the content commitment.
	tampered := cloneManifest(t, got)
	tampered.Segments[0].Shards[0].Hash =
		integrity.Digest([]byte("different shard")).String()

	if err := store.ReplaceLocations(ctx, got, tampered); err == nil {
		t.Fatal("accepted changed shard commitment")
	}

	final, err := store.Load(ctx, original.FileID)
	if err != nil {
		t.Fatal(err)
	}
	if final.Segments[0].Shards[0] != got.Segments[0].Shards[0] {
		t.Fatal("rejected update changed stored metadata")
	}
}
