package metadata

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

var ErrCompleteState = errors.New("complete metadata state unavailable, unsupported or over limit")

// CompleteStateDigest describes logical contents, not freshness or authority.
// Treat the digest as private; never log actual recovery-state fingerprints.
type CompleteStateDigest struct {
	Version uint32
	Schema  uint32
	Tables  uint32
	Rows    uint64
	SHA256  [32]byte
}

// Pin the exact schema produced by the existing version-8 migrations. Different
// DDL (even equivalent formatting) needs review, not automatic normalization.
const completeSchemaSHA256 = "ca07c04ced68800e1106d56cd47a7ee2006d2672d387372e7b1350197ae63300"

var completeTables = [...]string{
	"api_credentials", "file_access", "file_names", "manifests", "principals",
	"registration_jobs", "registration_spend_accounts", "registration_spend_reservations",
	"registration_transactions", "repair_jobs", "upload_leases",
}

type completeLimits struct{ rows, cell, total uint64 }

var defaultCompleteLimits = completeLimits{100000, 1 << 20, 64 << 20}

// DigestCompleteState reads one consistent schema-8 snapshot, including committed
// WAL, without creating/migrating the source. No runtime or restore controller
// calls it. It does NOT validate command semantics, authenticate a newest tip,
// archive overwritten history, enable writes, or grant recovery/signing permission.
// Queries get a two-minute context cap; backend I/O may still be uninterruptible.
// Go buffers are bounded; SQLite's internal/temp allocation is backend-dependent.
func DigestCompleteState(ctx context.Context, path string) (CompleteStateDigest, error) {
	return digestCompleteState(ctx, path, defaultCompleteLimits)
}

func digestCompleteState(ctx context.Context, path string, limits completeLimits) (result CompleteStateDigest, err error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	defer func() {
		if err != nil {
			result = CompleteStateDigest{}
			err = ErrCompleteState // No filenames, rows, hashes or raw driver errors.
			if ctx.Err() != nil {
				err = ctx.Err()
			}
		}
	}()
	if limits.rows == 0 || limits.rows > 100000 || limits.cell == 0 || limits.cell > 1<<20 || limits.total == 0 || limits.total > 64<<20 {
		return result, ErrCompleteState
	}
	s, err := OpenRestoreReadOnly(ctx, path)
	if err != nil {
		return result, err
	}
	defer s.Close()
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	var version, application int
	if err = tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil || version != 8 {
		return result, ErrCompleteState
	}
	if err = tx.QueryRowContext(ctx, "PRAGMA application_id").Scan(&application); err != nil || application != 0 {
		return result, ErrCompleteState
	}
	var encoding string
	if err = tx.QueryRowContext(ctx, "PRAGMA encoding").Scan(&encoding); err != nil || encoding != "UTF-8" {
		return result, ErrCompleteState
	}
	schema, err := completeSchema(ctx, tx)
	if err != nil || hex.EncodeToString(schema[:]) != completeSchemaSHA256 {
		return result, ErrCompleteState
	}
	var integrity string
	if err = tx.QueryRowContext(ctx, "PRAGMA integrity_check(1)").Scan(&integrity); err != nil || integrity != "ok" {
		return result, ErrCompleteState
	}
	fk, err := tx.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return result, err
	}
	badFK := fk.Next()
	fkErr := fk.Err()
	fk.Close()
	if badFK || fkErr != nil {
		return result, ErrCompleteState
	}
	h := sha256.New()
	h.Write([]byte("tank-complete-state-v1\x00"))
	h.Write(schema[:])
	completeUint(h, uint64(len(completeTables)))
	var total, count uint64
	for _, table := range completeTables {
		var n uint64
		n, err = completeTable(ctx, tx, h, table, limits, &total)
		if err != nil {
			return result, err
		}
		count += n
	}
	if err = ctx.Err(); err != nil {
		return result, err
	}
	if err = tx.Commit(); err != nil {
		return result, err
	}
	result = CompleteStateDigest{Version: 1, Schema: 8, Tables: uint32(len(completeTables)), Rows: count}
	copy(result.SHA256[:], h.Sum(nil))
	return result, nil
}

func completeUint(h hash.Hash, value uint64) {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], value)
	h.Write(b[:])
}
func completeBytes(h hash.Hash, value []byte)  { completeUint(h, uint64(len(value))); h.Write(value) }
func completeString(h hash.Hash, value string) { completeBytes(h, []byte(value)) }
func completeQuote(name string) string         { return `"` + strings.ReplaceAll(name, `"`, `""`) + `"` }

func completeSchema(ctx context.Context, tx *sql.Tx) ([32]byte, error) {
	// Bound metadata BEFORE Scan can allocate it. sqlite_* objects are derived
	// internal indexes; their constraints are included in original table DDL.
	rows, err := tx.QueryContext(ctx, `SELECT
 CASE WHEN length(CAST(type AS BLOB))<=16 THEN type END,
 CASE WHEN length(CAST(name AS BLOB))<=128 THEN name END,
 CASE WHEN length(CAST(tbl_name AS BLOB))<=128 THEN tbl_name END,
 CASE WHEN length(CAST(sql AS BLOB))<=16384 THEN sql END
 FROM sqlite_schema WHERE name NOT GLOB 'sqlite_*'
 ORDER BY type COLLATE BINARY,name COLLATE BINARY LIMIT 257`)
	if err != nil {
		return [32]byte{}, err
	}
	defer rows.Close()
	var objects [][4]string
	for rows.Next() {
		var object [4]string
		if len(objects) == 256 || rows.Scan(&object[0], &object[1], &object[2], &object[3]) != nil {
			return [32]byte{}, ErrCompleteState
		}
		objects = append(objects, object)
	}
	if err := rows.Err(); err != nil {
		return [32]byte{}, err
	}
	h := sha256.New()
	h.Write([]byte("tank-complete-schema-v1\x00"))
	completeUint(h, 8)
	completeUint(h, uint64(len(objects)))
	for _, object := range objects {
		for _, field := range object {
			completeString(h, field)
		}
	}
	var digest [32]byte
	copy(digest[:], h.Sum(nil))
	return digest, nil
}

type completeColumn struct {
	name, declared string
	required       bool
}

func completeColumns(ctx context.Context, tx *sql.Tx, table string) ([]completeColumn, error) {
	rows, err := tx.QueryContext(ctx, "PRAGMA table_info("+completeQuote(table)+")")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var columns []completeColumn
	for rows.Next() {
		var c completeColumn
		var cid, pk int
		var value sql.NullString
		if len(columns) == 16 || rows.Scan(&cid, &c.name, &c.declared, &c.required, &value, &pk) != nil || cid != len(columns) {
			return nil, ErrCompleteState
		}
		if c.declared != "TEXT" && c.declared != "INTEGER" && c.declared != "BLOB" {
			return nil, ErrCompleteState
		}
		columns = append(columns, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(columns) == 0 {
		return nil, ErrCompleteState
	}
	return columns, nil
}

func completeTable(ctx context.Context, tx *sql.Tx, h hash.Hash, table string, limits completeLimits, total *uint64) (uint64, error) {
	columns, err := completeColumns(ctx, tx, table)
	if err != nil {
		return 0, err
	}
	completeString(h, table)
	completeUint(h, uint64(len(columns)))
	var projection []string
	for _, c := range columns {
		completeString(h, c.name)
		completeString(h, c.declared)
		name := completeQuote(c.name)
		// Text length() stops at embedded NUL; use BLOB byte length instead.
		// Oversized cells return NULL plus their true class, and are rejected.
		projection = append(projection, fmt.Sprintf("CASE WHEN typeof(%s) IN ('null','integer') OR length(CAST(%s AS BLOB))<=%d THEN %s END", name, name, limits.cell, name), "typeof("+name+")")
	}
	query := fmt.Sprintf("SELECT %s FROM %s LIMIT %d", strings.Join(projection, ","), completeQuote(table), limits.rows+1)
	rows, err := tx.QueryContext(ctx, query)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	values, classes := make([]any, len(columns)), make([]string, len(columns))
	destinations := make([]any, 0, 2*len(columns))
	for i := range columns {
		destinations = append(destinations, &values[i], &classes[i])
	}
	var hashes [][32]byte
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		if uint64(len(hashes)) == limits.rows || rows.Scan(destinations...) != nil {
			return 0, ErrCompleteState
		}
		row := sha256.New()
		row.Write([]byte("tank-complete-row-v1\x00"))
		completeUint(row, uint64(len(columns)))
		for i, c := range columns {
			var size uint64
			switch classes[i] {
			case "null":
				if c.required || values[i] != nil {
					return 0, ErrCompleteState
				}
				row.Write([]byte{0})
			case "integer":
				v, ok := values[i].(int64)
				if !ok || c.declared != "INTEGER" {
					return 0, ErrCompleteState
				}
				row.Write([]byte{1})
				completeUint(row, uint64(v))
				size = 8
			case "text":
				v, ok := values[i].(string)
				if !ok || c.declared != "TEXT" || !utf8.ValidString(v) {
					return 0, ErrCompleteState
				}
				row.Write([]byte{2})
				completeString(row, v)
				size = uint64(len(v))
			case "blob":
				v, ok := values[i].([]byte)
				if !ok || c.declared != "BLOB" {
					return 0, ErrCompleteState
				}
				row.Write([]byte{3})
				completeBytes(row, v)
				size = uint64(len(v))
			default:
				return 0, ErrCompleteState
			}
			if size > limits.total-*total {
				return 0, ErrCompleteState
			}
			*total += size
		}
		var sum [32]byte
		copy(sum[:], row.Sum(nil))
		hashes = append(hashes, sum)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	// Sort bounded fixed-size row digests, not SQL values: row order, collation
	// ties and nullable PRIMARY KEYs cannot make equal logical multisets differ.
	sort.Slice(hashes, func(i, j int) bool { return bytes.Compare(hashes[i][:], hashes[j][:]) < 0 })
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	completeUint(h, uint64(len(hashes)))
	for _, sum := range hashes {
		h.Write(sum[:])
	}
	return uint64(len(hashes)), nil
}
