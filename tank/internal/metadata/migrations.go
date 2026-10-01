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

	if version > 1 {
		return fmt.Errorf("database schema %d is newer than supported schema 1", version)
	}

	if version == 0 {
		_, err := tx.ExecContext(ctx, `
			CREATE TABLE manifests (
				file_id TEXT PRIMARY KEY,
				body TEXT NOT NULL
			);
			PRAGMA user_version = 1;
		`)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}
