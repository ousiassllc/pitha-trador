package decisiontrade

import (
	"context"
	"fmt"

	"github.com/ousiassllc/pitha-trador/internal/domain/marketregime"
)

// ListRegimeTrades returns every closed position whose entry order was placed
// for a trade signal derived from a Jev trader decision that carries an A/B
// arm (issue #755), with the arm and the market regime the decision was made
// under, for internal/service/calibration's A/B comparison. Unlike List it
// does not require a directional Jev decision: a VWAP reversion entry is
// taken whatever Jev's own direction was. Positions without a signal/decision
// link (manual entries) and decisions made before the regime was assessed are
// excluded. ReturnPct is realized_pnl over the entry notional (entry_price *
// quantity), in percent.
func (r *Repository) ListRegimeTrades(ctx context.Context) ([]marketregime.Trade, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT d.ab_arm, COALESCE(d.market_regime, ''), p.realized_pnl, p.entry_price * p.quantity
FROM positions p
JOIN paper_orders o ON o.id = p.entry_order_id
JOIN trade_signals s ON s.id = o.trade_signal_id
JOIN jev_decisions d ON d.id = s.jev_decision_id
WHERE p.closed_at IS NOT NULL AND p.realized_pnl IS NOT NULL
	AND d.decision_type = 'trader' AND d.ab_arm IS NOT NULL`)
	if err != nil {
		return nil, fmt.Errorf("decisiontrade: list regime trades: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []marketregime.Trade
	for rows.Next() {
		var (
			t        marketregime.Trade
			notional float64
		)
		if err := rows.Scan(&t.ABArm, &t.Regime, &t.RealizedPnL, &notional); err != nil {
			return nil, fmt.Errorf("decisiontrade: scan regime trade: %w", err)
		}
		if notional > 0 {
			t.ReturnPct = t.RealizedPnL / notional * 100
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("decisiontrade: list regime trades: %w", err)
	}
	return out, nil
}
