package jquants

import (
	"context"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// ReserveRequest counts one request of (day, dataset) in research_jquants_fetch_log
// when the day's total across the datasets is still below limit, in one statement
// (so two callers cannot both take the last slot), and reports whether it did.
// The caller sends the request only when it returned true: a request is counted
// before it is sent, so a failed or interrupted one never lets the day exceed the
// limit. limit <= 0 reserves nothing.
func (r *Repository) ReserveRequest(ctx context.Context, day, dataset string, limit int, at time.Time) (bool, error) {
	if limit <= 0 {
		return false, nil
	}
	res, err := r.db.ExecContext(ctx, `INSERT INTO research_jquants_fetch_log (day, dataset, requests, last_request_at)
SELECT ?, ?, 1, ? WHERE (SELECT COALESCE(SUM(requests), 0) FROM research_jquants_fetch_log WHERE day = ?) < ?
ON CONFLICT (day, dataset) DO UPDATE SET requests = requests + 1, last_request_at = excluded.last_request_at`,
		day, dataset, sqlutil.FormatTime(at), day, limit)
	if err != nil {
		return false, fmt.Errorf("jquants: reserve a request of %s %s: %w", dataset, day, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("jquants: reserved rows: %w", err)
	}
	return n == 1, nil
}

// RecordFailure counts a failed request of (day, dataset) with its fixed error
// code (never a response body).
func (r *Repository) RecordFailure(ctx context.Context, day, dataset, code string) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO research_jquants_fetch_log (day, dataset, failures, last_failure_code) VALUES (?, ?, 1, ?)
ON CONFLICT (day, dataset) DO UPDATE SET failures = failures + 1, last_failure_code = excluded.last_failure_code`, day, dataset, code)
	if err != nil {
		return fmt.Errorf("jquants: record a failure of %s %s: %w", dataset, day, err)
	}
	return nil
}

// RecordSuccess adds the rows a completed fetch of (day, dataset) stored and
// stamps the success time; the last error code is cleared.
func (r *Repository) RecordSuccess(ctx context.Context, day, dataset string, rows int, at time.Time) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO research_jquants_fetch_log (day, dataset, rows_stored, last_success_at) VALUES (?, ?, ?, ?)
ON CONFLICT (day, dataset) DO UPDATE SET rows_stored = rows_stored + excluded.rows_stored, last_success_at = excluded.last_success_at, last_failure_code = ''`,
		day, dataset, rows, sqlutil.FormatTime(at))
	if err != nil {
		return fmt.Errorf("jquants: record a success of %s %s: %w", dataset, day, err)
	}
	return nil
}

// RequestsOn is the number of requests counted on the JST day.
func (r *Repository) RequestsOn(ctx context.Context, day string) (int, error) {
	var n int
	if err := r.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(requests), 0) FROM research_jquants_fetch_log WHERE day = ?`, day).Scan(&n); err != nil {
		return 0, fmt.Errorf("jquants: count requests of %s: %w", day, err)
	}
	return n, nil
}
