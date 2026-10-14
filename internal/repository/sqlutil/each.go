package sqlutil

import (
	"context"
	"database/sql"
	"fmt"
)

// Querier is satisfied by *sql.DB and *sql.Tx.
type Querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// Each runs query and calls fn with every row scanned by scan, in result
// order, without materializing the result set: the export reads (issue #770)
// span a whole period and must not hold every row in memory. The first error
// from scan or fn stops the iteration and is returned; the rows are always
// closed.
func Each[T any](ctx context.Context, q Querier, scan func(RowScanner) (T, error), fn func(T) error, query string, args ...any) error {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			return err
		}
		if err := fn(v); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("repository: iterate rows: %w", err)
	}
	return nil
}
