package metadata

import (
	"context"
	"database/sql"
	"fmt"
)

func migrate(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var version int
	if err := tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return err
	}

	if version > 3 {
		return fmt.Errorf("database schema %d is newer than supported schema 3", version)
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

	return tx.Commit()
}
