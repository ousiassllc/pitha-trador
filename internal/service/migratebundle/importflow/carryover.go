package importflow

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/service/backup"
	"github.com/ousiassllc/pitha-trador/internal/service/migratebundle"
)

// carryOver copies what belongs to THIS PC from the safety copy of the
// current database into the staged one, which the bundle deliberately lacks:
// the secrets rows (the operator typed them on the Setup screen, which has to
// be completed before Settings - and so the import - can be reached) and the
// machine-specific runtime settings (migratebundle.ExcludedSettingKeys: the
// backup/log directories, the 秘密鍵 paths). The values stay on this PC: they
// go from one local file to another, never into a bundle. A current database
// that predates a table or has no such rows simply contributes nothing.
func carryOver(ctx context.Context, staged, from string) (err error) {
	conn, err := backup.OpenCopy(staged)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, conn.Close()) }()
	if _, err := conn.ExecContext(ctx, "ATTACH DATABASE ? AS cur", sqlitedb.FileURI(from, "mode=ro")); err != nil {
		return fmt.Errorf("carry over this PC's settings: attach the safety copy: %w", err)
	}
	// Probed before the transaction: the pool holds one connection.
	hasSecrets, hasSettings := has(ctx, conn, "secrets"), has(ctx, conn, "runtime_settings")
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("carry over this PC's settings: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if hasSecrets {
		if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO main.secrets (key, encrypted_value, updated_at) SELECT key, encrypted_value, updated_at FROM cur.secrets`); err != nil {
			return fmt.Errorf("carry over this PC's secrets: %w", err)
		}
	}
	if hasSettings {
		for _, key := range migratebundle.ExcludedSettingKeys() {
			if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO main.runtime_settings (key, value, updated_at) SELECT key, value, updated_at FROM cur.runtime_settings WHERE key = ?`, key); err != nil {
				return fmt.Errorf("carry over this PC's machine-specific settings: %w", err)
			}
		}
	}
	return tx.Commit()
}

// migrateStaged opens the staged database the way the application will
// (sqlitedb.Open applies every pending golang-migrate migration), folds its
// WAL back into the file and closes it, so a bundle from an older release is
// migrated - and a migration that fails is found - BEFORE the live database
// is replaced.
func migrateStaged(ctx context.Context, staged string) error {
	conn, err := sqlitedb.Open(staged)
	if err != nil {
		return fmt.Errorf("migrate the imported database to this version: %w", err)
	}
	_, err = conn.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)")
	return errors.Join(err, conn.Close())
}

// has reports whether the attached current database has the table.
func has(ctx context.Context, conn *sql.DB, table string) bool {
	var n int
	return conn.QueryRowContext(ctx, `SELECT count(*) FROM cur.sqlite_master WHERE type='table' AND name = ?`, table).Scan(&n) == nil && n == 1
}
