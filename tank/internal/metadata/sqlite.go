package metadata

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

var (
	ErrNotFound = errors.New("manifest not found")
	ErrExists   = errors.New("manifest already exists")
)

type Store struct {
	db *sql.DB
}

func Open(ctx context.Context, path string) (*Store, error) {
	if path == "" {
		return nil, fmt.Errorf("database path is required")
	}

	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}

	if err := os.MkdirAll(filepath.Dir(absolute), 0700); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", absolute)
	if err != nil {
		return nil, err
	}

	// One connection keeps connection-specific settings consistent.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	fail := func(err error) (*Store, error) {
		db.Close()
		return nil, err
	}

	if err := db.PingContext(ctx); err != nil {
		return fail(err)
	}

	for _, statement := range []string{
		"PRAGMA busy_timeout = 5000",
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = FULL",
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fail(err)
		}
	}

	if err := migrate(ctx, db); err != nil {
		return fail(err)
	}

	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

// Save inserts a manifest without overwriting an existing file record.
func (s *Store) Save(ctx context.Context, m Manifest) error {
	if err := m.Validate(); err != nil {
		return err
	}

	body, err := json.Marshal(m)
	if err != nil {
		return err
	}

	result, err := s.db.ExecContext(ctx, `
		INSERT INTO manifests (file_id, body)
		VALUES (?, ?)
		ON CONFLICT(file_id) DO NOTHING
	`, m.FileID, string(body))
	if err != nil {
		return err
	}

	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrExists
	}

	return nil
}

func (s *Store) Load(ctx context.Context, id string) (Manifest, error) {
	var m Manifest
	if !validHash(id) {
		return m, fmt.Errorf("invalid file ID")
	}

	var body string
	err := s.db.QueryRowContext(ctx,
		"SELECT body FROM manifests WHERE file_id = ?",
		id,
	).Scan(&body)

	if errors.Is(err, sql.ErrNoRows) {
		return m, ErrNotFound
	}
	if err != nil {
		return m, err
	}
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		return m, err
	}
	if err := m.Validate(); err != nil {
		return m, err
	}
	if m.FileID != id {
		return m, fmt.Errorf("stored manifest ID mismatch")
	}

	return m, nil
}

// List returns a bounded, deterministic page of file IDs.
// Use the last returned ID as "after" for the next page.
func (s *Store) List(ctx context.Context, after string, limit int) ([]string, error) {
	if limit < 1 || limit > 1000 {
		return nil, fmt.Errorf("limit must be between 1 and 1000")
	}
	if after != "" && !validHash(after) {
		return nil, fmt.Errorf("invalid pagination cursor")
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT file_id FROM manifests
		WHERE file_id > ?
		ORDER BY file_id
		LIMIT ?
	`, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}

	return ids, rows.Err()
}
