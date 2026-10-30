// Package sidecar opens a second connection pool on the application's SQLite
// file for background recorders that must stay out of trading's way (issue #935).
package sidecar

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
)

// busyTimeout is how long a sidecar statement waits for the write lock
// held by the application's own connections before it fails. It is short: a
// sidecar write that cannot get the lock is dropped rather than made to wait.
const busyTimeout = 5 * time.Second

// Open opens a second connection pool on the existing, already migrated
// database file at path, for background recorders that must stay out of the
// application's way (issue #935: the disclosure skip record). It differs from
// sqlitedb.Open in three ways, all so that the recorder cannot touch trading:
//
//   - it does not use the instrumented driver, so the recorder's statements,
//     failed or not, never feed DBWriteFailures (the FR-RISK-2 DB-write
//     kill switch reads that streak, and a success here would also reset it);
//   - it never creates the file and runs no migration;
//   - its busy timeout is 5 seconds, not 60, and the pool holds one connection,
//     so a recorder statement holds at most one write lock at a time and gives
//     up quickly.
//
// Callers are responsible for closing the returned *sql.DB.
func Open(path string) (*sql.DB, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("repository: open sidecar database %q: %w", path, err)
	}
	conn, err := sql.Open("sqlite", sqlitedb.FileURI(path, fmt.Sprintf("_foreign_keys=1&_journal_mode=WAL&_busy_timeout=%d&_txlock=immediate", busyTimeout.Milliseconds())))
	if err != nil {
		return nil, fmt.Errorf("repository: open sidecar database %q: %w", path, err)
	}
	conn.SetMaxOpenConns(1)
	if err := conn.PingContext(context.Background()); err != nil {
		return nil, errors.Join(fmt.Errorf("repository: connect to sidecar database %q: %w", path, err), conn.Close())
	}
	return conn, nil
}
