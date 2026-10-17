package trading

import (
	"context"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// ClosedReturn is one closed position's realized return.
type ClosedReturn struct {
	// ExitReason is positions.exit_reason (domain.ExitReason*; empty when unset).
	ExitReason string
	// ReturnBps is realized_pnl (net of fees) over the entry notional
	// (entry_price * quantity), in bps.
	ReturnBps float64
}

// ListClosedReturns returns the realized return of every position closed in
// [from, to): realized_pnl (net of both fills' fees) over the entry notional,
// in bps, with its exit reason. Positions whose entry notional is not positive
// are left out. It reads positions only; nothing is stored.
func (r *PositionRepository) ListClosedReturns(ctx context.Context, from, to time.Time) ([]ClosedReturn, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT COALESCE(exit_reason, ''), realized_pnl * 10000.0 / (entry_price * quantity)
FROM positions
WHERE closed_at IS NOT NULL AND realized_pnl IS NOT NULL AND entry_price * quantity > 0
	AND closed_at >= ? AND closed_at < ?
ORDER BY closed_at ASC, id ASC`, sqlutil.FormatTime(from), sqlutil.FormatTime(to))
	if err != nil {
		return nil, fmt.Errorf("repository: list closed position returns in [%s, %s): %w", from.UTC().Format(time.RFC3339), to.UTC().Format(time.RFC3339), err)
	}
	defer func() { _ = rows.Close() }()

	var out []ClosedReturn
	for rows.Next() {
		var c ClosedReturn
		if err := rows.Scan(&c.ExitReason, &c.ReturnBps); err != nil {
			return nil, fmt.Errorf("repository: scan closed position return: %w", err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: list closed position returns: %w", err)
	}
	return out, nil
}
