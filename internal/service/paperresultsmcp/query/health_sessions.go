package query

import (
	"context"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
)

// sessionGaps splits the TSE trading days (JST) of r that have no
// market_snapshots row (issue #814). A day with an earlier snapshot is a real
// gap (missing). A day with no snapshot before it lies before the history
// began, e.g. just after the first start, so nothing can be said about it
// (insufficient). Both lists are empty, never nil, and in date order.
func sessionGaps(ctx context.Context, q Q, r Range) (missing, insufficient []string, err error) {
	missing, insufficient = []string{}, []string{}
	for day := r.From.In(JST); day.Before(r.To); day = day.AddDate(0, 0, 1) {
		if !marketcalendar.TSE.IsTradingDay(day) {
			continue
		}
		next := day.AddDate(0, 0, 1)
		onDay, err := snapshotExists(ctx, q, day, next)
		if err != nil {
			return nil, nil, err
		}
		if onDay {
			continue
		}
		// The history starts at the first snapshot ever stored: any snapshot
		// before this day (from the zero time) proves the system was collecting.
		earlier, err := snapshotExists(ctx, q, time.Time{}, day)
		if err != nil {
			return nil, nil, err
		}
		if earlier {
			missing = append(missing, day.Format(time.DateOnly))
		} else {
			insufficient = append(insufficient, day.Format(time.DateOnly))
		}
	}
	return missing, insufficient, nil
}

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
