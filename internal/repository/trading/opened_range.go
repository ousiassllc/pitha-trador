package trading

import (
	"context"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// ListOpenedBetween returns every position opened in [start, end), open or
// closed, oldest first: the Paper entries the backtest's reproducibility check
// (internal/service/backtest/parity) compares a replay with.
func (r *PositionRepository) ListOpenedBetween(ctx context.Context, start, end time.Time) ([]domain.Position, error) {
	rows, err := r.db.QueryContext(ctx,
		positionSelectColumns+` WHERE opened_at >= ? AND opened_at < ? ORDER BY opened_at ASC, id ASC`,
		sqlutil.FormatTime(start), sqlutil.FormatTime(end),
	)
	if err != nil {
		return nil, fmt.Errorf("repository: list positions opened between %s and %s: %w", start, end, err)
	}
	return scanPositions(rows)
}
