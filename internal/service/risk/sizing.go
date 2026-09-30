package risk

import (
	"context"
	"log/slog"

	"github.com/ousiassllc/pitha-trador/internal/service/risk/sizing"
)

// PositionSize returns the order quantity for a new entry at price
// (sizing.Quantity against the live total exposure). Quantity 0 means no
// entry is allowed - no lot fits the limits, or the exposure could not be
// read (fail closed); the reason is logged.
func (e *Engine) PositionSize(ctx context.Context, price float64) int64 {
	total, err := e.portfolio.TotalExposurePct(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "risk: read total exposure for sizing failed; entry skipped", "error", err)
		return 0
	}
	qty, reason := sizing.Quantity(e.limits, e.stopLossPct, price, total)
	if qty == 0 {
		slog.InfoContext(ctx, "risk: no lot fits the limits; entry skipped", "reason", reason, "price", price)
	}
	return qty
}

// AllowedPositionPct returns the largest position, as a percentage of
// account equity, an entry at price may take right now (the sizing result
// x price / equity), or 0 when not even one lot is allowed. Symbol
// Detail's allowed_position_pct.
func (e *Engine) AllowedPositionPct(ctx context.Context, price float64) float64 {
	qty := e.PositionSize(ctx, price)
	if qty == 0 {
		return 0
	}
	return float64(qty) * price / e.limits.InitialCapital * 100
}
