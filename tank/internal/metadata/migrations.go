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

	if version > 2 {
		return fmt.Errorf("database schema %d is newer than supported schema 2", version)
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

			PRAGMA user_version = 2;
		`); err != nil {
			return err
		}
	}

	return tx.Commit()
}
