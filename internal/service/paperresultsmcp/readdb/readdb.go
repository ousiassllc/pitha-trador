// Package readdb opens the application's SQLite file for the read-only Paper
// results MCP (issue #783, docs/architecture/paper-results-mcp.md §4).
//
// It deliberately does not use sqlitedb.Open, which creates the file, sets
// journal_mode=WAL and runs the migrations. Here the file is never created,
// no migration or wal_checkpoint runs, the connection is mode=ro with
// PRAGMA query_only=ON, busy_timeout is 1 second and the pool holds one
// connection.
package readdb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"time"

	_ "modernc.org/sqlite" // registers the "sqlite" database/sql driver

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
)

// DriverName is the database/sql driver Open uses.
const DriverName = "sqlite"

// BusyTimeout is how long a read waits for a lock held by the application
// (architecture §4: 1 second, not the application's 60).
const BusyTimeout = time.Second

// ErrUnavailable wraps every failure to open or verify the database file.
var ErrUnavailable = errors.New("paper-results-mcp: database unavailable")

// dsnQuery: mode=ro refuses writes and file creation; _pragma applies
// query_only on every connection the pool opens, not just the first.
const dsnQuery = "mode=ro&_busy_timeout=1000&_pragma=query_only(ON)"

// Open opens the existing database file at path read-only. It fails with
// ErrUnavailable when the file is missing (it is never created) or when
// PRAGMA query_only is not ON afterwards.
func Open(path string) (*sql.DB, error) {
	return OpenWith(DriverName, path)
}

// OpenWith is Open over a caller-chosen registered driver name; tests pass a
// statement-counting wrapper of the "sqlite" driver.
func OpenWith(driverName, path string) (*sql.DB, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("%w: path is a directory", ErrUnavailable)
	}
	conn, err := sql.Open(driverName, sqlitedb.FileURI(path, dsnQuery))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	conn.SetMaxOpenConns(1)

	// The single pooled connection is the one every tool uses, so checking
	// it here checks them all (a replacement connection gets the same DSN).
	var queryOnly int
	if err := conn.QueryRowContext(context.Background(), "PRAGMA query_only").Scan(&queryOnly); err != nil {
		return nil, errors.Join(fmt.Errorf("%w: %w", ErrUnavailable, err), conn.Close())
	}
	if queryOnly != 1 {
		return nil, errors.Join(fmt.Errorf("%w: query_only is not ON", ErrUnavailable), conn.Close())
	}
	return conn, nil
}
