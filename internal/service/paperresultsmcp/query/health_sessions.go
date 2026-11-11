package query

import (
	"context"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// snapshotExists reports whether market_snapshots has a row in [from, to).
func snapshotExists(ctx context.Context, q Q, from, to time.Time) (bool, error) {
	var exists bool
	err := q.QueryRowContext(ctx, `
SELECT EXISTS (SELECT 1 FROM market_snapshots WHERE timestamp >= ? AND timestamp < ?)`,
		sqlutil.FormatTime(from), sqlutil.FormatTime(to)).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("query: snapshot existence: %w", err)
	}
	return exists, nil
}
