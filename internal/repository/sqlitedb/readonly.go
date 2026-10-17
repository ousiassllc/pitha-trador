package sqlitedb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
)

// OpenReadOnly opens the existing database file at path for reading only:
// no migration runs, the file is never created and a write fails. It is for
// offline analysis tools (cmd/dailyguardreplay) that must not touch the
// application's data; the database file itself is never modified, but on a
// WAL database SQLite creates the empty -wal and the -shm index beside it
// and leaves them after Close (a read-only connection cannot delete them),
// so the directory must be writable. The database may be at any schema
// version, so callers must tolerate a missing table. Callers are responsible
// for closing the returned *sql.DB.
func OpenReadOnly(path string) (*sql.DB, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("repository: open database %q read-only: %w", path, err)
	}
	conn, err := sql.Open("sqlite", FileURI(path, "mode=ro&_busy_timeout=5000"))
	if err != nil {
		return nil, fmt.Errorf("repository: open sqlite database %q read-only: %w", path, err)
	}
	conn.SetMaxOpenConns(1)
	if err := conn.PingContext(context.Background()); err != nil {
		return nil, errors.Join(fmt.Errorf("repository: connect to sqlite database %q read-only: %w", path, err), conn.Close())
	}
	return conn, nil
}
