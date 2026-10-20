package mcpset

import (
	"context"
	"database/sql"
	"fmt"
)

// runInTx runs fn in one transaction on db: it commits when fn succeeds and
// rolls everything back when fn fails, so a setting and its audit row are
// stored together or not at all (issue #813). what names the transaction in
// the begin / commit errors. Gate and Writer both run their changes here.
func runInTx(ctx context.Context, db *sql.DB, what string, fn func(tx *sql.Tx) error) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("mcpset: begin %s: %w", what, err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("mcpset: commit %s: %w", what, err)
	}
	return nil
}
