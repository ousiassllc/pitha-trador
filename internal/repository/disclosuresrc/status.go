package disclosuresrc

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain/disclosuresrc"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// BeginRequest counts one request of source on the JST day `day` against the
// daily cap and reports whether it may be sent. The count is made BEFORE the
// request (a failed request counts too) and in one statement, so two callers
// can never both take the last slot. allowed=false means the cap is used up:
// nothing is counted and the request must not be sent.
func (r *Repository) BeginRequest(ctx context.Context, source, day string, capPerDay int, now time.Time) (allowed bool, err error) {
	res, err := r.db.ExecContext(ctx, `
INSERT INTO research_disclosure_source_status (source, requests_day, requests_today, request_count, last_fetched_at)
VALUES (?, ?, 1, 1, ?)
ON CONFLICT (source) DO UPDATE SET
	requests_today = CASE WHEN requests_day = excluded.requests_day THEN requests_today + 1 ELSE 1 END,
	requests_day = excluded.requests_day,
	request_count = request_count + 1,
	last_fetched_at = excluded.last_fetched_at
WHERE requests_day <> excluded.requests_day OR requests_today < ?`,
		source, day, sqlutil.FormatTime(now), capPerDay)
	if err != nil {
		return false, fmt.Errorf("disclosuresrc: count %s request: %w", source, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("disclosuresrc: count %s request: rows affected: %w", source, err)
	}
	return n > 0, nil
}

// RecordSuccess marks the last request of source as successful.
func (r *Repository) RecordSuccess(ctx context.Context, source string, now time.Time) error {
	return r.exec(ctx, "record success", `
UPDATE research_disclosure_source_status SET last_success_at = ? WHERE source = ?`, sqlutil.FormatTime(now), source)
}

// RecordFailure counts a failed request of source with its short fixed code
// (disclosuresrc.Fail* / HTTPFailureCode). No error text is stored.
func (r *Repository) RecordFailure(ctx context.Context, source, code string, now time.Time) error {
	return r.exec(ctx, "record failure", `
UPDATE research_disclosure_source_status SET failure_count = failure_count + 1, last_failure_at = ?, last_failure_code = ?
WHERE source = ?`, sqlutil.FormatTime(now), code, source)
}

func (r *Repository) exec(ctx context.Context, what, query string, args ...any) error {
	if _, err := r.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("disclosuresrc: %s: %w", what, err)
	}
	return nil
}

// Status returns the fetch state of source; a source never fetched has the zero
// state (only Source set).
func (r *Repository) Status(ctx context.Context, source string) (disclosuresrc.SourceStatus, error) {
	st := disclosuresrc.SourceStatus{Source: source}
	var fetched, success, failure sql.NullString
	err := r.db.QueryRowContext(ctx, `
SELECT requests_day, requests_today, request_count, failure_count, last_fetched_at, last_success_at, last_failure_at, last_failure_code
FROM research_disclosure_source_status WHERE source = ?`, source).
		Scan(&st.RequestsDay, &st.RequestsToday, &st.RequestCount, &st.FailureCount, &fetched, &success, &failure, &st.LastFailureCode)
	if errors.Is(err, sql.ErrNoRows) {
		return st, nil
	}
	if err != nil {
		return st, fmt.Errorf("disclosuresrc: read %s status: %w", source, err)
	}
	if st.LastFetchedAt, err = sqlutil.ParseNullableTime(fetched); err != nil {
		return st, err
	}
	if st.LastSuccessAt, err = sqlutil.ParseNullableTime(success); err != nil {
		return st, err
	}
	if st.LastFailureAt, err = sqlutil.ParseNullableTime(failure); err != nil {
		return st, err
	}
	return st, nil
}
