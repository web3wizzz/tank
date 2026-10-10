package metadata

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func TestMigrationWaitsForWriter(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	dbPath := filepath.Join(t.TempDir(), "tank.sqlite")
	store, err := Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	writer, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()

	conn, err := writer.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		t.Fatal(err)
	}
	defer conn.ExecContext(context.Background(), "ROLLBACK")

	// Keep a version-3 schema change uncommitted.
	if _, err := conn.ExecContext(ctx,
		"DROP TABLE upload_leases; DROP TABLE file_access; DROP TABLE api_credentials; DROP TABLE principals; DROP TABLE file_names; PRAGMA user_version = 3;",
	); err != nil {
		t.Fatal(err)
	}

	peer, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	peer.SetMaxOpenConns(1)

	if _, err := peer.ExecContext(ctx, "PRAGMA busy_timeout = 5000"); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		done <- migrate(ctx, peer)
	}()

	select {
	case err := <-done:
		t.Fatalf("migration returned before the writer released its lock: %v", err)
	case <-time.After(150 * time.Millisecond):
	}

	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}

	var version int
	if err := peer.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != 6 {
		t.Fatalf("schema version = %d, want 6", version)
	}
}
