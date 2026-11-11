package sessiongap

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
)

// Querier is what Observe needs of a database: *sql.DB, *sql.Tx and the MCP's
// read-only query.Q all satisfy it.
type Querier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Observe reads day's market_snapshots (timestamps only; never a price or raw
// column) and classifies the day. It issues one index seek per minute with a
// snapshot plus one for the history check, so a trading day costs a few hundred
// cheap statements however many instruments each minute holds. It does not
// count the rows (Observation.Rows stays 0; see CountRows), which would scan
// every instrument's row.
func Observe(ctx context.Context, q Querier, day time.Time) (Observation, error) {
	sessions := marketcalendar.TSE.Sessions(day)
	if len(sessions) != 2 {
		return Classify(day, nil, nil, false), nil
	}
	dayStart := time.Date(sessions[0].Open.Year(), sessions[0].Open.Month(), sessions[0].Open.Day(), 0, 0, 0, 0, marketcalendar.JST)
	var hasEarlier bool
	err := q.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM market_snapshots WHERE timestamp < ?)`,
		sqlutil.FormatTime(dayStart)).Scan(&hasEarlier)
	if err != nil {
		return Observation{}, fmt.Errorf("sessiongap: earlier snapshot: %w", err)
	}
	var minutes [2][]time.Time
	for i, s := range sessions {
		if minutes[i], err = snapshotMinutes(ctx, q, s); err != nil {
			return Observation{}, err
		}
	}
	return Classify(day, minutes[0], minutes[1], hasEarlier), nil
}

// CountRows is the number of market_snapshots rows inside day's sessions.
func CountRows(ctx context.Context, q Querier, day time.Time) (int, error) {
	total := 0
	for _, s := range marketcalendar.TSE.Sessions(day) {
		var n int
		err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM market_snapshots WHERE timestamp >= ? AND timestamp < ?`,
			sqlutil.FormatTime(s.Open), sqlutil.FormatTime(s.Close)).Scan(&n)
		if err != nil {
			return 0, fmt.Errorf("sessiongap: snapshot rows: %w", err)
		}
		total += n
	}
	return total, nil
}

// snapshotMinutes lists the distinct minutes (UTC instants) of s that have a
// snapshot, ascending, by seeking from one snapshot to the next minute.
func snapshotMinutes(ctx context.Context, q Querier, s marketcalendar.Session) ([]time.Time, error) {
	var out []time.Time
	end := sqlutil.FormatTime(s.Close)
	for cur := s.Open.UTC(); cur.Before(s.Close); {
		var first sql.NullString
		err := q.QueryRowContext(ctx, `SELECT MIN(timestamp) FROM market_snapshots WHERE timestamp >= ? AND timestamp < ?`,
			sqlutil.FormatTime(cur), end).Scan(&first)
		if err != nil {
			return nil, fmt.Errorf("sessiongap: next snapshot: %w", err)
		}
		if !first.Valid {
			break
		}
		at, err := sqlutil.ParseTime(first.String)
		if err != nil {
			return nil, err
		}
		minute := at.Truncate(time.Minute)
		out = append(out, minute)
		cur = minute.Add(time.Minute)
	}
	return out, nil
}
