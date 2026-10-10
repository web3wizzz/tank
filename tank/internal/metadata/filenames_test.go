package metadata

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"tank.local/tank/internal/encoding"
	"tank.local/tank/internal/integrity"
)

func filenameTestManifest(t *testing.T) Manifest {
	t.Helper()
	data := []byte("Tank filename persistence test")
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
		records[i] = Shard{Index: i, Hash: integrity.Digest(shard).String()}
	}
	return Manifest{
		Version: 1,
		FileID:  integrity.Digest(data).String(),
		Size:    int64(len(data)),
		Segments: []Segment{{
			Index: 0, Size: len(data),
			MerkleRoot: root.String(), Shards: records,
		}},
	}
}

func TestFilenamePersistenceAndCompatibility(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "tank.sqlite")
	store, err := Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}

	m := filenameTestManifest(t)
	if err := store.Save(ctx, m); err != nil {
		t.Fatal(err)
	}
	if name, err := store.LoadFilename(ctx, m.FileID); err != nil || name != "" {
		t.Fatalf("legacy unnamed record: name=%q err=%v", name, err)
	}
	before, err := m.Commitment()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	// Simulate the existing version-3 database before filename support.
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_, migrationErr := db.ExecContext(ctx,
		"DROP TABLE registration_transactions; DROP TABLE upload_leases; DROP TABLE file_access; DROP TABLE api_credentials; DROP TABLE principals; DROP TABLE file_names; PRAGMA user_version = 3;")
	closeErr := db.Close()
	if migrationErr != nil {
		t.Fatal(migrationErr)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}

	store, err = Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveFilename(ctx, m.FileID, `C:\fakepath\report.pdf`); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveFilename(ctx, m.FileID, "renamed.pdf"); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store, err = Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	name, err := store.LoadFilename(ctx, m.FileID)
	if err != nil || name != "report.pdf" {
		t.Fatalf("persisted first filename: name=%q err=%v", name, err)
	}
	loaded, err := store.Load(ctx, m.FileID)
	if err != nil {
		t.Fatal(err)
	}
	after, err := loaded.Commitment()
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatal("filename changed the file commitment")
	}
	if err := store.SaveFilename(ctx, strings.Repeat("0", 64), "missing.pdf"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing file: %v", err)
	}
}

func TestNormalizeFilename(t *testing.T) {
	for _, name := range []string{
		"", ".", "..", "bad\r\nname.pdf", strings.Repeat("x", 256),
		"hidden\u202ename.pdf",
	} {
		if _, err := NormalizeFilename(name); !errors.Is(err, ErrInvalidFilename) {
			t.Errorf("expected rejection for %q, got %v", name, err)
		}
	}
	for _, name := range []string{"report.pdf", "résumé.pdf", "报告.pdf"} {
		got, err := NormalizeFilename(name)
		if err != nil || got != name {
			t.Errorf("name=%q got=%q err=%v", name, got, err)
		}
	}
}
