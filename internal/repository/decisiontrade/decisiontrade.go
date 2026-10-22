// Package decisiontrade reads closed positions joined back to the Jev
// trader decision that opened them, for Calibration's confidence-bucket
// PnL (functional.md FR-CAL-2). It spans the judgement and trading resource
// groups, so it is its own package rather than part of either (sibling
// packages under internal/repository never import each other).
package decisiontrade

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// Repository lists DecisionTrades from the SQLite database.
type Repository struct {
	db *sql.DB
	// foreign scopes List (WithForeignTraderVersions, issue #881).
	foreign sqlutil.ForeignTraderVersions
}

// WithForeignTraderVersions makes List leave out the positions opened by a Trader
// decision of the versions foreign returns (the prompt versions of another
// cohort than the live one), so the confidence-bucket PnL sits beside the
// Calibration samples of the same prompt (calibration_samples.go). nil leaves
// every position in. It returns r, for use at construction.
func (r *Repository) WithForeignTraderVersions(foreign sqlutil.ForeignTraderVersions) *Repository {
	r.foreign = foreign
	return r
}

// New returns a Repository backed by db.
func New(db *sql.DB) *Repository {
	return &Repository{db: db}
}

// List returns every closed position whose entry order was
// placed for a trade signal derived from a directional Jev trader
// decision (positions.entry_order_id -> paper_orders.trade_signal_id ->
// trade_signals.jev_decision_id), for internal/service/calibration.
// WithTradePnL to aggregate into FR-CAL-2's "confidence bucket別PnL".
// Positions without a signal/decision link (manual entries) are excluded.
// ReturnPct is realized_pnl over the entry notional (entry_price *
// quantity), in percent.
func (r *Repository) List(ctx context.Context) ([]domain.DecisionTrade, error) {
	query := `
SELECT d.confidence, p.realized_pnl, p.entry_price * p.quantity
FROM positions p
JOIN paper_orders o ON o.id = p.entry_order_id
JOIN trade_signals s ON s.id = o.trade_signal_id
JOIN jev_decisions d ON d.id = s.jev_decision_id
WHERE p.closed_at IS NOT NULL AND p.realized_pnl IS NOT NULL
	AND d.decision_type = 'trader' AND d.direction IN ('LONG', 'SHORT') AND d.confidence IS NOT NULL`
	var args []any
	if r.foreign != nil {
		versions, err := r.foreign(ctx)
		if err != nil {
			return nil, fmt.Errorf("decisiontrade: resolve the Trader prompt versions to leave out: %w", err)
		}
		if cond, condArgs := sqlutil.NotIn("d.question_version", versions); cond != "" {
			query += " AND " + cond //nolint:gosec // G202: cond is a package-internal NOT IN fragment; the versions are bound parameters
			args = condArgs
		}
	}
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("decisiontrade: list: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []domain.DecisionTrade
	for rows.Next() {
		var (
			t        domain.DecisionTrade
			notional float64
		)
		if err := rows.Scan(&t.Confidence, &t.RealizedPnL, &notional); err != nil {
			return nil, fmt.Errorf("decisiontrade: scan: %w", err)
		}
		if notional > 0 {
			t.ReturnPct = t.RealizedPnL / notional * 100
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("decisiontrade: list: %w", err)
	}
	return out, nil
}
