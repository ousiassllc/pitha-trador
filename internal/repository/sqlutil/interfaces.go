package sqlutil

import (
	"context"
	"database/sql"
)

// RowScanner is satisfied by both *sql.Row and *sql.Rows, letting scan
// helpers work for single-row and multi-row queries alike.
type RowScanner interface {
	Scan(dest ...any) error
}

// Execer is satisfied by both *sql.DB and *sql.Tx, letting a statement
// run standalone or as part of a caller-managed transaction.
type Execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// Executor is satisfied by both *sql.DB and *sql.Tx, letting a repository
// statement that also reads a single row run standalone or inside a
// caller's transaction.
type Executor interface {
	Execer
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}
