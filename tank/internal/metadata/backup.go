package metadata

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
)

// BackupDatabase reads an existing database without migrating or changing it.
// This permits a pre-upgrade snapshot even when the source schema is older.
func BackupDatabase(ctx context.Context, source, destination string) error {
	info, err := os.Lstat(source)
	if err != nil || !info.Mode().IsRegular() {
		return errors.New("backup source must be an existing regular database")
	}
	absolute, err := filepath.Abs(source)
	if err != nil {
		return err
	}
	uri := (&url.URL{Scheme: "file", Path: filepath.ToSlash(absolute), RawQuery: "mode=ro"}).String()
	db, err := sql.Open("sqlite", uri)
	if err != nil {
		return errors.New("could not open metadata for backup")
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err := db.ExecContext(ctx, "PRAGMA busy_timeout = 5000"); err != nil {
		return errors.New("could not read metadata for backup")
	}
	return (&Store{db: db}).Backup(ctx, destination)
}

// Backup publishes a consistent SQLite snapshot without stopping live writers.
// Snapshots contain credential hashes and permissions: keep them private.
// An existing destination (including a symlink) is never replaced.
func (s *Store) Backup(ctx context.Context, destination string) error {
	if destination == "" {
		return errors.New("backup destination is required")
	}
	destination, err := filepath.Abs(destination)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(destination); err == nil {
		return errors.New("backup destination already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	parent := filepath.Dir(destination)
	// Require an operator-created directory rather than silently changing paths.
	directory, err := os.Open(parent)
	if err != nil {
		return fmt.Errorf("open backup directory: %w", err)
	}
	defer directory.Close()
	info, err := directory.Stat()
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("backup parent is not a directory")
	}
	staging, err := os.MkdirTemp(parent, ".tank-backup-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging) // Only the new directory owned by this operation.
	snapshot := filepath.Join(staging, "snapshot.sqlite")
	// VACUUM INTO provides a transactionally consistent snapshot of WAL state.
	// Binding the path prevents SQL injection from operator-supplied filenames.
	if _, err := s.db.ExecContext(ctx, "VACUUM INTO ?", snapshot); err != nil {
		return fmt.Errorf("snapshot metadata: %w", err)
	}
	if err := os.Chmod(snapshot, 0600); err != nil {
		return err
	}
	verification, err := sql.Open("sqlite", (&url.URL{Scheme: "file", Path: filepath.ToSlash(snapshot), RawQuery: "mode=ro"}).String())
	if err != nil {
		return err
	}
	defer verification.Close()
	var result string
	if err := verification.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&result); err != nil {
		return fmt.Errorf("verify snapshot: %w", err)
	}
	if result != "ok" {
		return errors.New("snapshot integrity check failed")
	}
	rows, err := verification.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return err
	}
	if rows.Next() {
		rows.Close()
		return errors.New("snapshot foreign key check failed")
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if err := verification.Close(); err != nil {
		return err
	}
	file, err := os.Open(snapshot)
	if err != nil {
		return err
	}
	err = file.Sync()
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// Atomic no-replace publication on the same filesystem, including a race with
	// another writer choosing this destination. File mode remains 0600.
	if err := os.Link(snapshot, destination); err != nil {
		return fmt.Errorf("publish private snapshot: %w", err)
	}
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("snapshot published but directory sync failed: %w", err)
	}
	return nil
}
