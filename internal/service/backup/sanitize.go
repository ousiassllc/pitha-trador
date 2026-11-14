package backup

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"

	_ "modernc.org/sqlite" // registers the "sqlite" database/sql driver (also registered by internal/repository)
)

// scrubSecrets removes every row of the secrets table from the
// backup copy at path and rewrites the file so the deleted values do not
// linger in free pages. The secrets table is only encrypted with the
// application-embedded key (config.EncryptSecret), so a copy carrying it
// would hand broker/API credentials to whatever the backup destination
// syncs to; after a restore the operator re-enters them on the Setup
// screen. The copy is also switched to journal_mode=DELETE so no -wal/-shm
// sidecar files outlive the operation. prune (may be nil) runs after the
// secrets are deleted and before the VACUUM, so whatever else the caller
// removes from the copy (the PC migration bundle's machine-specific
// settings) is rewritten out of the file's free pages too.
func scrubSecrets(ctx context.Context, path string, prune Prune) (err error) {
	conn, err := OpenCopy(path)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, conn.Close()) }()

	for _, stmt := range []string{
		"PRAGMA journal_mode=DELETE",
		"PRAGMA secure_delete=ON",
		"DELETE FROM secrets",
	} {
		if _, err := conn.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("backup: scrub backup %q (%s): %w", path, stmt, err)
		}
	}
	if prune != nil {
		if err := prune(ctx, conn); err != nil {
			return fmt.Errorf("backup: scrub backup %q: %w", path, err)
		}
	}
	if _, err := conn.ExecContext(ctx, "VACUUM"); err != nil {
		return fmt.Errorf("backup: scrub backup %q (VACUUM): %w", path, err)
	}
	return nil
}

// VerifyCopy runs `PRAGMA integrity_check` against the database copy at
// path (no migration, never created) and returns an error unless SQLite
// reports "ok".
func VerifyCopy(ctx context.Context, path string) (err error) {
	conn, err := OpenCopy(path)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, conn.Close()) }()

	var result string
	if err := conn.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&result); err != nil {
		return fmt.Errorf("backup: integrity check of backup %q: %w", path, err)
	}
	if result != "ok" {
		return fmt.Errorf("backup: backup %q failed integrity check: %s", path, result)
	}
	return nil
}

// OpenCopy opens the database copy at path (a backup, a migration bundle's
// database) with a single plain connection (no migrations, no instrumented
// driver: it is not the live database; mode=rw never creates the file).
// Callers must close it.
func OpenCopy(path string) (*sql.DB, error) {
	conn, err := sql.Open("sqlite", sqlitedb.FileURI(path, "mode=rw&_busy_timeout=60000"))
	if err != nil {
		return nil, fmt.Errorf("backup: open backup %q: %w", path, err)
	}
	conn.SetMaxOpenConns(1)
	return conn, nil
}
