package metadata

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

var (
	ErrUnauthorized = errors.New("invalid or expired credential")
	ErrAccessDenied = errors.New("file access denied")
)

type Principal struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type AccessKey struct {
	ID          string    `json:"id"`
	PrincipalID string    `json:"principal_id"`
	ExpiresAt   time.Time `json:"expires_at"`
}

type FileAccess struct {
	FileID   string `json:"file_id"`
	Filename string `json:"filename"`
	Size     int64  `json:"size"`
}

func accessRandomHex(size int) (string, error) {
	bytes := make([]byte, size)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

// CreateAccessKey creates a user and their first 30-day credential.
// The raw credential is returned once and is never stored in the database.
func (s *Store) CreateAccessKey(
	ctx context.Context, label string,
) (AccessKey, string, error) {
	var empty AccessKey
	label = strings.TrimSpace(label)
	if label == "" || len(label) > 80 || !utf8.ValidString(label) {
		return empty, "", fmt.Errorf("user label must contain 1 to 80 UTF-8 bytes")
	}
	for _, r := range label {
		if unicode.IsControl(r) {
			return empty, "", fmt.Errorf("invalid user label")
		}
	}

	userID, err := accessRandomHex(16)
	if err != nil {
		return empty, "", err
	}
	keyID, err := accessRandomHex(16)
	if err != nil {
		return empty, "", err
	}
	secret, err := accessRandomHex(32)
	if err != nil {
		return empty, "", err
	}

	token := "tank_u_" + secret
	hash := sha256.Sum256([]byte(token))
	now := time.Now().UTC()
	key := AccessKey{
		ID:          "key_" + keyID,
		PrincipalID: "usr_" + userID,
		ExpiresAt:   now.Add(30 * 24 * time.Hour).Truncate(time.Second),
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return empty, "", err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx,
		"INSERT INTO principals (id, label) VALUES (?, ?)",
		key.PrincipalID, label,
	); err != nil {
		return empty, "", err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO api_credentials
			(id, principal_id, token_hash, expires_at, revoked)
		VALUES (?, ?, ?, ?, 0)
	`, key.ID, key.PrincipalID, hex.EncodeToString(hash[:]),
		key.ExpiresAt.Unix()); err != nil {
		return empty, "", err
	}
	if err := tx.Commit(); err != nil {
		return empty, "", err
	}

	return key, token, nil
}

func (s *Store) AuthenticateAccessKey(
	ctx context.Context, token string,
) (Principal, error) {
	var principal Principal
	if len(token) != 71 || !strings.HasPrefix(token, "tank_u_") {
		return principal, ErrUnauthorized
	}
	if _, err := hex.DecodeString(token[7:]); err != nil {
		return principal, ErrUnauthorized
	}

	hash := sha256.Sum256([]byte(token))
	err := s.db.QueryRowContext(ctx, `
		SELECT p.id, p.label
		FROM api_credentials AS c
		JOIN principals AS p ON p.id = c.principal_id
		WHERE c.token_hash = ? AND c.revoked = 0 AND c.expires_at > ?
	`, hex.EncodeToString(hash[:]), time.Now().Unix()).
		Scan(&principal.ID, &principal.Label)

	if errors.Is(err, sql.ErrNoRows) {
		return Principal{}, ErrUnauthorized
	}
	return principal, err
}

func (s *Store) RevokeAccessKey(ctx context.Context, keyID string) error {
	result, err := s.db.ExecContext(ctx,
		"UPDATE api_credentials SET revoked = 1 WHERE id = ?", keyID,
	)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrNotFound
	}
	return nil
}

// GrantFileAccess is an internal operation, not a public "claim by ID" API.
// Call it only after a successful authorized tanking operation.
func (s *Store) GrantFileAccess(
	ctx context.Context, principalID, id, filename string,
) error {
	if !validHash(id) {
		return ErrAccessDenied
	}
	if filename != "" {
		var err error
		filename, err = NormalizeFilename(filename)
		if err != nil {
			return err
		}
	}

	result, err := s.db.ExecContext(ctx, `
		INSERT INTO file_access (principal_id, file_id, filename)
		SELECT p.id, m.file_id, ?
		FROM principals AS p CROSS JOIN manifests AS m
		WHERE p.id = ? AND m.file_id = ?
		ON CONFLICT(principal_id, file_id) DO UPDATE SET
			filename = CASE
				WHEN file_access.filename = '' THEN excluded.filename
				ELSE file_access.filename
			END
	`, filename, principalID, id)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrAccessDenied
	}
	return nil
}

func (s *Store) LoadFileAccess(
	ctx context.Context, principalID, id string,
) (FileAccess, error) {
	var access FileAccess
	if !validHash(id) {
		return access, ErrAccessDenied
	}

	err := s.db.QueryRowContext(ctx, `
		SELECT filename FROM file_access
		WHERE principal_id = ? AND file_id = ?
	`, principalID, id).Scan(&access.Filename)

	if errors.Is(err, sql.ErrNoRows) {
		return access, ErrAccessDenied
	}
	if err != nil {
		return access, err
	}

	m, err := s.Load(ctx, id)
	if err != nil {
		return access, err
	}
	access.FileID = id
	access.Size = m.Size
	if access.Filename == "" {
		access.Filename = id + ".bin"
	}
	return access, nil
}

func (s *Store) ListAccessibleFiles(
	ctx context.Context, principalID, after string,
) ([]string, error) {
	if after != "" && !validHash(after) {
		return nil, fmt.Errorf("invalid pagination cursor")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT a.file_id
		FROM file_access AS a JOIN manifests AS m ON m.file_id = a.file_id
		WHERE a.principal_id = ? AND a.file_id > ?
		ORDER BY a.file_id LIMIT 100
	`, principalID, after)
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
