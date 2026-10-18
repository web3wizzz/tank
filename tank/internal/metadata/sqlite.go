package metadata

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"syscall"

	_ "modernc.org/sqlite"
)

var (
	ErrNotFound       = errors.New("manifest not found")
	ErrExists         = errors.New("manifest already exists")
	ErrRestoreBlocked = errors.New("inactive or unsafe database path; writable access blocked")
)

type Store struct {
	db *sql.DB
}

func Open(ctx context.Context, path string) (*Store, error) {
	return openDatabase(ctx, path, false)
}

// OpenExisting refuses to bootstrap a missing or uninitialized authority database.
// It does not prove that an existing database is the newest authoritative state.
func OpenExisting(ctx context.Context, path string) (*Store, error) {
	return openDatabase(ctx, path, true)
}

func openDatabase(ctx context.Context, path string, existing bool) (*Store, error) {
	if path == "" {
		return nil, fmt.Errorf("database path is required")
	}

	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}

	if err := checkDatabaseRestoreGate(absolute); err != nil {
		return nil, err
	}
	if !existing {
		if err := os.MkdirAll(filepath.Dir(absolute), 0700); err != nil {
			return nil, err
		}
	}

	location := &url.URL{Scheme: "file", Path: filepath.ToSlash(absolute)}
	if existing {
		location.RawQuery = "mode=rw"
	}
	db, err := sql.Open("sqlite", location.String())
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

	if existing {
		var version int
		if err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
			return fail(err)
		}
		if version == 0 {
			return fail(fmt.Errorf("existing database is uninitialized; refusing authority bootstrap"))
		}
	}

	if err := checkDatabaseRestoreGate(absolute); err != nil {
		return fail(err)
	}

	for _, statement := range []string{
		"PRAGMA busy_timeout = 5000",
		"PRAGMA foreign_keys = ON",
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

// Marker presence (including malformed or inaccessible markers) blocks writable
// opens. Follow database aliases so a symlink cannot bypass its target's gate.
// This does not authenticate freshness or fence stores that are already open.
func checkDatabaseRestoreGate(path string) error {
	for depth := 0; depth < 40; depth++ {
		if _, err := os.Lstat(path + ".restore-gate"); !os.IsNotExist(err) {
			return ErrRestoreBlocked
		}
		// Resolve each final-component symlink separately so intermediate
		// alias markers cannot be skipped. Parent symlinks are also followed
		// by Lstat on the marker's path.
		info, err := os.Lstat(path)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return ErrRestoreBlocked
		}
		if info.Mode()&os.ModeSymlink == 0 {
			// SQLite journal/lock naming cannot safely coordinate hard-link
			// aliases, which could also omit a candidate's adjacent marker.
			stat, ok := info.Sys().(*syscall.Stat_t)
			if !info.Mode().IsRegular() || !ok || stat.Nlink != 1 {
				return ErrRestoreBlocked
			}
			return nil
		}
		target, err := os.Readlink(path)
		if err != nil {
			return ErrRestoreBlocked
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(path), target)
		}
		path = filepath.Clean(target)
	}
	return ErrRestoreBlocked
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
