package metadata

import (
	"context"
	"database/sql"
	"errors"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"
)

var ErrInvalidFilename = errors.New("invalid filename")

// NormalizeFilename keeps a safe basename, including its extension.
func NormalizeFilename(name string) (string, error) {
	if !utf8.ValidString(name) {
		return "", ErrInvalidFilename
	}
	for _, r := range name {
		if unicode.IsControl(r) ||
			(r >= '\u202a' && r <= '\u202e') ||
			(r >= '\u2066' && r <= '\u2069') {
			return "", ErrInvalidFilename
		}
	}

	name = path.Base(strings.ReplaceAll(strings.TrimSpace(name), `\`, "/"))
	if name == "" || name == "." || name == ".." || name == "/" ||
		len(name) > 255 {
		return "", ErrInvalidFilename
	}
	return name, nil
}

// SaveFilename preserves the first name associated with existing file bytes.
func (s *Store) SaveFilename(ctx context.Context, id, name string) error {
	if !validHash(id) {
		return ErrInvalidFilename
	}
	name, err := NormalizeFilename(name)
	if err != nil {
		return err
	}

	result, err := s.db.ExecContext(ctx, `
		INSERT INTO file_names (file_id, filename)
		SELECT file_id, ? FROM manifests WHERE file_id = ?
		ON CONFLICT(file_id) DO NOTHING
	`, name, id)
	if err != nil {
		return err
	}

	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		// Distinguish an existing name from a missing file.
		_, err = s.Load(ctx, id)
		return err
	}
	return nil
}

// LoadFilename returns an empty name for older, unnamed records.
func (s *Store) LoadFilename(ctx context.Context, id string) (string, error) {
	if !validHash(id) {
		return "", ErrInvalidFilename
	}
	var name string
	err := s.db.QueryRowContext(ctx,
		"SELECT filename FROM file_names WHERE file_id = ?", id,
	).Scan(&name)

	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return NormalizeFilename(name)
}
