package metadata

import (
	"context"
	"database/sql"
	"fmt"
)

func migrate(ctx context.Context, db *sql.DB) error {
	// Pin one connection for the entire migration transaction.
	tx, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer tx.Close()

	// Acquire the write lock before reading the schema version.
	if _, err := tx.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return err
	}

	committed := false
	defer func() {
		if !committed {
			_, _ = tx.ExecContext(context.Background(), "ROLLBACK")
		}
	}()

	var version int
	if err := tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return err
	}

	if version > 5 {
		return fmt.Errorf("database schema %d is newer than supported schema 5", version)
	}

	if version == 0 {
		if _, err := tx.ExecContext(ctx, `
			CREATE TABLE manifests (
				file_id TEXT PRIMARY KEY,
				body TEXT NOT NULL
			);
		`); err != nil {
			return err
		}
	}

	if version < 2 {
		if _, err := tx.ExecContext(ctx, `
			CREATE TABLE repair_jobs (
				file_id TEXT PRIMARY KEY,
				attempts INTEGER NOT NULL DEFAULT 0,
				next_attempt INTEGER NOT NULL,
				lease_until INTEGER NOT NULL DEFAULT 0,
				lease_token TEXT NOT NULL DEFAULT '',
				last_error TEXT NOT NULL DEFAULT ''
			);

			CREATE INDEX repair_jobs_due
			ON repair_jobs(next_attempt, lease_until);
		`); err != nil {
			return err
		}
	}

	if version < 3 {
		if _, err := tx.ExecContext(ctx, `
			CREATE TABLE registration_jobs (
				file_id TEXT NOT NULL,
				target TEXT NOT NULL,
				status TEXT NOT NULL DEFAULT 'pending'
					CHECK(status IN ('pending', 'submitted', 'registered', 'failed')),
				tx_hash TEXT NOT NULL DEFAULT '',
				attempts INTEGER NOT NULL DEFAULT 0,
				next_attempt INTEGER NOT NULL,
				lease_until INTEGER NOT NULL DEFAULT 0,
				lease_token TEXT NOT NULL DEFAULT '',
				last_error TEXT NOT NULL DEFAULT '',
				PRIMARY KEY(file_id, target)
			);

			CREATE INDEX registration_jobs_due
			ON registration_jobs(target, status, next_attempt, lease_until);

			PRAGMA user_version = 3;
		`); err != nil {
			return err
		}
	}

	if version < 4 {
		if _, err := tx.ExecContext(ctx, `
			CREATE TABLE file_names (
				file_id TEXT PRIMARY KEY REFERENCES manifests(file_id),
				filename TEXT NOT NULL
			);
			PRAGMA user_version = 4;
		`); err != nil {
			return err
		}
	}

	if version < 5 {
		if _, err := tx.ExecContext(ctx, `
			CREATE TABLE principals (
				id TEXT PRIMARY KEY,
				label TEXT NOT NULL
			);

			CREATE TABLE api_credentials (
				id TEXT PRIMARY KEY,
				principal_id TEXT NOT NULL REFERENCES principals(id),
				token_hash TEXT NOT NULL UNIQUE,
				expires_at INTEGER NOT NULL,
				revoked INTEGER NOT NULL DEFAULT 0
					CHECK(revoked IN (0, 1))
			);

			CREATE TABLE file_access (
				principal_id TEXT NOT NULL REFERENCES principals(id),
				file_id TEXT NOT NULL REFERENCES manifests(file_id),
				filename TEXT NOT NULL DEFAULT '',
				PRIMARY KEY(principal_id, file_id)
			);

			PRAGMA user_version = 5;
		`); err != nil {
			return err
		}
	}

	if _, err := tx.ExecContext(ctx, "COMMIT"); err != nil {
		return err
	}
	committed = true
	return nil
}
