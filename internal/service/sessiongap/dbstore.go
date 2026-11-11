package sessiongap

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
)

// DBStore persists the daily session diagnostics in session_diagnostics_daily
// (migration 000052; issue #1054). It implements Store. The app writes the
// table; the Paper results MCP reads it.
type DBStore struct {
	db *sql.DB
}

// NewDBStore returns a DBStore backed by db.
func NewDBStore(db *sql.DB) *DBStore {
	return &DBStore{db: db}
}

var _ Store = (*DBStore)(nil)

// Finalized reports whether date has a verdict row (one that is not in progress).
func (r *DBStore) Finalized(ctx context.Context, date string) (bool, error) {
	var done bool
	err := r.db.QueryRowContext(ctx, `
SELECT EXISTS (SELECT 1 FROM session_diagnostics_daily WHERE session_date = ? AND gap_kind <> ?)`,
		date, string(KindInProgress)).Scan(&done)
	if err != nil {
		return false, fmt.Errorf("sessiongap: session diagnostics of %s: %w", date, err)
	}
	return done, nil
}

// Unfinished lists the in-progress dates before beforeDate, oldest first.
func (r *DBStore) Unfinished(ctx context.Context, beforeDate string) ([]string, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT session_date FROM session_diagnostics_daily WHERE gap_kind = ? AND session_date < ? ORDER BY session_date`,
		string(KindInProgress), beforeDate)
	if err != nil {
		return nil, fmt.Errorf("sessiongap: list unfinished session diagnostics: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var date string
		if err := rows.Scan(&date); err != nil {
			return nil, fmt.Errorf("sessiongap: scan unfinished session diagnostics: %w", err)
		}
		out = append(out, date)
	}
	return out, rows.Err()
}

// Touch merges alertCodes into the in-progress row of date (creating it) and
// counts one watch check when inSession. A finished row is left as it is. The
// Recorder is the only writer and runs one pass at a time, so the read and the
// write need no transaction.
func (r *DBStore) Touch(ctx context.Context, now time.Time, date string, alertCodes []string, inSession bool) error {
	var kind, stored string
	err := r.db.QueryRowContext(ctx, `SELECT gap_kind, alert_codes_json FROM session_diagnostics_daily WHERE session_date = ?`, date).Scan(&kind, &stored)
	switch {
	case err == nil && kind != string(KindInProgress):
		return nil
	case err != nil && !errors.Is(err, sql.ErrNoRows):
		return fmt.Errorf("sessiongap: read session diagnostics of %s: %w", date, err)
	}
	var codes []string
	if stored != "" {
		if err := json.Unmarshal([]byte(stored), &codes); err != nil {
			return fmt.Errorf("sessiongap: decode alert codes of %s: %w", date, err)
		}
	}
	for _, c := range alertCodes {
		if !slices.Contains(codes, c) {
			codes = append(codes, c)
		}
	}
	slices.Sort(codes)
	encoded, err := json.Marshal(nonNil(codes))
	if err != nil {
		return fmt.Errorf("sessiongap: encode alert codes: %w", err)
	}
	check := 0
	if inSession {
		check = 1
	}
	if _, err = r.db.ExecContext(ctx, `
INSERT INTO session_diagnostics_daily (session_date, gap_kind, alert_codes_json, watch_checks, recorded_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(session_date) DO UPDATE SET alert_codes_json = excluded.alert_codes_json,
	watch_checks = session_diagnostics_daily.watch_checks + excluded.watch_checks, recorded_at = excluded.recorded_at`,
		date, string(KindInProgress), string(encoded), check, sqlutil.FormatTime(now)); err != nil {
		return fmt.Errorf("sessiongap: touch session diagnostics of %s: %w", date, err)
	}
	return nil
}

// Observe classifies day from market_snapshots.
func (r *DBStore) Observe(ctx context.Context, day time.Time) (Observation, error) {
	o, err := Observe(ctx, r.db, day)
	if err != nil {
		return Observation{}, err
	}
	o.Rows, err = CountRows(ctx, r.db, day)
	return o, err
}

// MarketDataJobs counts the market-data jobs that finished on the JST day.
func (r *DBStore) MarketDataJobs(ctx context.Context, day time.Time) (succeeded, failed int, err error) {
	day = day.In(marketcalendar.JST)
	from := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, marketcalendar.JST)
	err = r.db.QueryRowContext(ctx, `
SELECT COALESCE(SUM(status = 'succeeded'), 0), COALESCE(SUM(status = 'failed'), 0) FROM jobs
WHERE queue = 'market-data' AND status IN ('succeeded', 'failed') AND finished_at >= ? AND finished_at < ?`,
		sqlutil.FormatTime(from), sqlutil.FormatTime(from.AddDate(0, 0, 1))).Scan(&succeeded, &failed)
	if err != nil {
		return 0, 0, fmt.Errorf("sessiongap: market-data jobs of %s: %w", day.Format(time.DateOnly), err)
	}
	return succeeded, failed, nil
}

// Finalize records the verdict of rec's day. A row that is already finished is
// kept (the first verdict stands); an in-progress row keeps the alert codes and
// watch checks it accumulated.
func (r *DBStore) Finalize(ctx context.Context, rec Record) error {
	codes, err := json.Marshal(nonNil(rec.Codes))
	if err != nil {
		return fmt.Errorf("sessiongap: encode gap codes: %w", err)
	}
	type span struct {
		Code    string `json:"code"`
		From    string `json:"from"`
		To      string `json:"to"`
		Minutes int    `json:"minutes"`
	}
	spans := make([]span, 0, len(rec.Gaps))
	for _, g := range rec.Gaps {
		spans = append(spans, span{g.Code, g.From.UTC().Format(time.RFC3339), g.To.UTC().Format(time.RFC3339), g.Minutes})
	}
	gaps, err := json.Marshal(spans)
	if err != nil {
		return fmt.Errorf("sessiongap: encode gaps: %w", err)
	}
	var first, last any
	if !rec.FirstAt.IsZero() {
		first, last = sqlutil.FormatTime(rec.FirstAt), sqlutil.FormatTime(rec.LastAt)
	}
	_, err = r.db.ExecContext(ctx, `
INSERT INTO session_diagnostics_daily (session_date, gap_kind, gap_codes_json, gaps_json, snapshot_rows, snapshot_minutes,
	first_snapshot_at, last_snapshot_at, longest_gap_minutes, market_data_jobs_succeeded, market_data_jobs_failed, recorded_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(session_date) DO UPDATE SET gap_kind = excluded.gap_kind, gap_codes_json = excluded.gap_codes_json,
	gaps_json = excluded.gaps_json, snapshot_rows = excluded.snapshot_rows, snapshot_minutes = excluded.snapshot_minutes,
	first_snapshot_at = excluded.first_snapshot_at, last_snapshot_at = excluded.last_snapshot_at,
	longest_gap_minutes = excluded.longest_gap_minutes, market_data_jobs_succeeded = excluded.market_data_jobs_succeeded,
	market_data_jobs_failed = excluded.market_data_jobs_failed, recorded_at = excluded.recorded_at
WHERE session_diagnostics_daily.gap_kind = ?`,
		rec.Date, string(rec.Kind), string(codes), string(gaps), rec.Rows, rec.Minutes, first, last, rec.LongestGapMinutes,
		rec.JobsSucceeded, rec.JobsFailed, sqlutil.FormatTime(rec.RecordedAt), string(KindInProgress))
	if err != nil {
		return fmt.Errorf("sessiongap: record session diagnostics of %s: %w", rec.Date, err)
	}
	return nil
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
