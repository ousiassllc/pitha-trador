// Package trialview is the 成績 ＞ お試し tab's database-backed source: it
// implements internal/web/handler/trialresults.Source over the application's
// database with the aggregation the Paper results MCP's jev_trial_compare and
// jev_trial_cost use (internal/service/paperresultsmcp/query/trialquery, called
// here and not modified), so the screen and those tools show the same figures.
// Every read runs in one read-only transaction and only issues SELECT
// statements.
package trialview

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query/trialquery"
)

// Reader implements trialresults.Source over DB.
type Reader struct {
	DB *sql.DB
}

// readTx runs fn in one read-only transaction; it is only ever rolled back.
func (r Reader) readTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return fmt.Errorf("trialview: begin read-only transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	return fn(tx)
}

// Compare is jev_trial_compare's data over r for every arm with a row.
func (r Reader) Compare(ctx context.Context, rng query.Range) (trialquery.Comparison, error) {
	var out trialquery.Comparison
	err := r.readTx(ctx, func(tx *sql.Tx) (err error) {
		out, err = trialquery.Compare(ctx, tx, rng, nil)
		return err
	})
	return out, err
}

// Cost is jev_trial_cost's data over r for every arm with a row, with the
// caps and stop state as of now.
func (r Reader) Cost(ctx context.Context, rng query.Range, now time.Time) (trialquery.CostReport, error) {
	var out trialquery.CostReport
	err := r.readTx(ctx, func(tx *sql.Tx) (err error) {
		out, err = trialquery.Cost(ctx, tx, rng, now, nil)
		return err
	})
	return out, err
}
