package query

import (
	"context"
	"fmt"
)

// CountClosed is how many positions closed in r: the very set PerformanceSummary
// aggregates (loadClosed's WHERE: closed_at and realized_pnl both set, closed_at
// in [From, To)), counted in SQL instead of read row by row. So no
// MaxClosedScan limit applies: a listing can show the trade counts of a period
// too long to aggregate.
func CountClosed(ctx context.Context, q Q, r Range) (int, error) {
	from, to := r.bounds()
	var n int
	err := q.QueryRowContext(ctx, `
SELECT COUNT(*) FROM positions
WHERE closed_at IS NOT NULL AND realized_pnl IS NOT NULL AND closed_at >= ? AND closed_at < ?`, from, to).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("query: count closed positions: %w", err)
	}
	return n, nil
}
