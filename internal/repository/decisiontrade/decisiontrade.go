// Package decisiontrade reads closed positions joined back to the Jev
// trader decision that opened them, for Calibration's confidence-bucket
// PnL (functional.md FR-CAL-2). It lives beside, not inside,
// internal/repository to keep that directory under the 2000-line rule.
package decisiontrade

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// Repository lists DecisionTrades from the SQLite database.
type Repository struct {
	db *sql.DB
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
	rows, err := r.db.QueryContext(ctx, `
SELECT d.confidence, p.realized_pnl, p.entry_price * p.quantity
FROM positions p
JOIN paper_orders o ON o.id = p.entry_order_id
JOIN trade_signals s ON s.id = o.trade_signal_id
JOIN jev_decisions d ON d.id = s.jev_decision_id
WHERE p.closed_at IS NOT NULL AND p.realized_pnl IS NOT NULL
	AND d.decision_type = 'trader' AND d.direction IN ('LONG', 'SHORT') AND d.confidence IS NOT NULL`)
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
