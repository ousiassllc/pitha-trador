package sessiondiag

import (
	"context"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// marketDataJobsByDay counts the market-data jobs finished in [from, to) by
// JST day and status; the (queue, status, finished_at) index covers it.
func marketDataJobsByDay(ctx context.Context, q query.Q, from, to string) (succeeded, failed map[string]int, err error) {
	succeeded, failed = map[string]int{}, map[string]int{}
	rows, err := q.QueryContext(ctx, `
SELECT `+jstDay+`, status, COUNT(*) FROM jobs
WHERE queue = 'market-data' AND status IN ('succeeded', 'failed') AND finished_at >= ? AND finished_at < ?
GROUP BY 1, 2`, from, to)
	if err != nil {
		return nil, nil, fmt.Errorf("query: market-data jobs by day: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var day, status string
		var n int
		if err := rows.Scan(&day, &status, &n); err != nil {
			return nil, nil, fmt.Errorf("query: scan market-data jobs by day: %w", err)
		}
		if status == "succeeded" {
			succeeded[day] = n
		} else {
			failed[day] = n
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("query: market-data jobs by day: %w", err)
	}
	return succeeded, failed, nil
}

// otherQueueJobsByDay counts the jobs of the queues other than market-data
// (and the retired feature-calc) that finished in [from, to), by JST day.
func otherQueueJobsByDay(ctx context.Context, q query.Q, from, to string) (map[string]int, error) {
	counts := map[string]int{}
	for _, queue := range query.Queues {
		if queue == "market-data" || queue == "feature-calc" {
			continue
		}
		rows, err := q.QueryContext(ctx, `
SELECT `+jstDay+`, COUNT(*) FROM jobs
WHERE queue = ? AND status IN ('succeeded', 'failed') AND finished_at >= ? AND finished_at < ?
GROUP BY 1`, queue, from, to)
		if err != nil {
			return nil, fmt.Errorf("query: %s jobs by day: %w", queue, err)
		}
		for rows.Next() {
			var day string
			var n int
			if err := rows.Scan(&day, &n); err != nil {
				_ = rows.Close()
				return nil, fmt.Errorf("query: scan %s jobs by day: %w", queue, err)
			}
			counts[day] += n
		}
		err = rows.Err()
		_ = rows.Close()
		if err != nil {
			return nil, fmt.Errorf("query: %s jobs by day: %w", queue, err)
		}
	}
	return counts, nil
}

// watchListsByDay reads the watch lists of the JST dates [from, to).
func watchListsByDay(ctx context.Context, q query.Q, from, to string) (map[string]WatchListDay, error) {
	lists := map[string]WatchListDay{}
	rows, err := q.QueryContext(ctx, `
SELECT l.list_date, l.source, (SELECT COUNT(*) FROM watch_list_entries e WHERE e.list_date = l.list_date)
FROM watch_lists l WHERE l.list_date >= ? AND l.list_date < ?`, from, to)
	if err != nil {
		return nil, fmt.Errorf("query: watch lists: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var date, source string
		var n int
		if err := rows.Scan(&date, &source, &n); err != nil {
			return nil, fmt.Errorf("query: scan watch lists: %w", err)
		}
		lists[date] = WatchListDay{Present: true, Source: &source, Entries: n}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("query: watch lists: %w", err)
	}
	return lists, nil
}

// succeededWithoutSnapshot counts the market-data jobs that succeeded on day
// and left no market_snapshots row for their instrument between their start
// and finish (the handler stamps the row with its own clock inside that
// span). The instrument comes from a fixed key of the payload (never
// returned), read only when the payload is valid JSON; a job whose payload
// names no instrument is counted as having no row.
func succeededWithoutSnapshot(ctx context.Context, q query.Q, day time.Time) (int, error) {
	from, to := bounds(day, day.AddDate(0, 0, 1))
	var n int
	err := q.QueryRowContext(ctx, `
SELECT COUNT(*) FROM jobs j
WHERE j.queue = 'market-data' AND j.status = 'succeeded' AND j.finished_at >= ? AND j.finished_at < ?
	AND NOT EXISTS (
		SELECT 1 FROM market_snapshots s
		WHERE s.instrument_id = CASE WHEN json_valid(j.payload_json) THEN CAST(json_extract(j.payload_json, '$.instrument_id') AS INTEGER) END
			AND s.timestamp >= j.started_at AND s.timestamp <= j.finished_at)`,
		from, to).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("query: succeeded jobs without a snapshot: %w", err)
	}
	return n, nil
}

// bounds are [from, to) as the application stores a time column.
func bounds(from, to time.Time) (string, string) {
	return sqlutil.FormatTime(from), sqlutil.FormatTime(to)
}
