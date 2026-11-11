// Package sessionrecord reads the daily session record the app keeps in
// session_diagnostics_daily (migration 000052; issue #1054) and judges each
// trading day's completeness for health_today, collection_status and
// session_diagnostics. It has its own small query interface so that query
// (which holds those tools) and sessiondiag can import it while it stays
// independent of query. It only reads; the app's sessiongap.Recorder writes.
package sessionrecord

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
	"github.com/ousiassllc/pitha-trador/internal/service/sessiongap"
)

// MaxRows caps the rows a read returns (the tools' period limit).
const MaxRows = 366

// Q is the read-only connection (query.Q satisfies it).
type Q interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

var jst = marketcalendar.JST

// formatOut renders a stored time as RFC3339 UTC (query.FormatOut).
func formatOut(stored string) string {
	t, err := sqlutil.ParseTime(stored)
	if err != nil {
		return stored
	}
	return t.UTC().Format(time.RFC3339)
}

func formatOutPtr(stored sql.NullString) *string {
	if !stored.Valid {
		return nil
	}
	s := formatOut(stored.String)
	return &s
}

// Gap is one stretch of a session with no snapshot (issue #1054).
type Gap struct {
	Code    string `json:"code"`
	From    string `json:"from"`
	To      string `json:"to"`
	Minutes int    `json:"minutes"`
}

// Diagnostic is one row of session_diagnostics_daily (migration 000052):
// the day's "session completeness" the app recorded after the close. It is kept
// beyond the succeeded-job retention and holds counts, times and fixed codes
// only, never a price. GapKind is sessiongap.Kind: "none", "whole_day",
// "intraday" or "insufficient_history" (#814: not a real gap).
type Diagnostic struct {
	Date    string   `json:"date"`
	GapKind string   `json:"gap_kind"`
	Codes   []string `json:"gap_codes"`
	Gaps    []Gap    `json:"gaps"`
	// AlertCodes are the health alert codes the app saw during the day; empty
	// either when none was active or when the app was not running to see one
	// (WatchChecks says which: few or no checks mean it was not running).
	AlertCodes        []string `json:"alert_codes"`
	WatchChecks       int      `json:"watch_checks"`
	SnapshotRows      int      `json:"snapshot_rows"`
	SnapshotMinutes   int      `json:"snapshot_minutes"`
	FirstSnapshotAt   *string  `json:"first_snapshot_at"`
	LastSnapshotAt    *string  `json:"last_snapshot_at"`
	LongestGapMinutes int      `json:"longest_gap_minutes"`
	// JobsSucceeded and JobsFailed count the market-data jobs finished that
	// day when the record was made; null when the jobs were already purged.
	JobsSucceeded *int   `json:"market_data_jobs_succeeded"`
	JobsFailed    *int   `json:"market_data_jobs_failed"`
	RecordedAt    string `json:"recorded_at"`
}

// Read reads the finished daily records of the JST dates [from,
// to), oldest first, at most limit rows. An in-progress row (today's, still
// being observed) is not returned.
func Read(ctx context.Context, q Q, from, to time.Time, limit int) ([]Diagnostic, error) {
	rows, err := q.QueryContext(ctx, `
SELECT session_date, gap_kind, gap_codes_json, gaps_json, alert_codes_json, watch_checks, snapshot_rows, snapshot_minutes,
	first_snapshot_at, last_snapshot_at, longest_gap_minutes, market_data_jobs_succeeded, market_data_jobs_failed, recorded_at
FROM session_diagnostics_daily
WHERE session_date >= ? AND session_date < ? AND gap_kind <> ?
ORDER BY session_date LIMIT ?`,
		from.In(jst).Format(time.DateOnly), to.In(jst).Format(time.DateOnly), string(sessiongap.KindInProgress), limit)
	if err != nil {
		return nil, fmt.Errorf("query: daily session diagnostics: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []Diagnostic{}
	for rows.Next() {
		var (
			d                             Diagnostic
			codes, gaps, alerts, recorded string
			first, last                   sql.NullString
			jobsOK, jobsFailed            sql.NullInt64
		)
		if err := rows.Scan(&d.Date, &d.GapKind, &codes, &gaps, &alerts, &d.WatchChecks, &d.SnapshotRows, &d.SnapshotMinutes,
			&first, &last, &d.LongestGapMinutes, &jobsOK, &jobsFailed, &recorded); err != nil {
			return nil, fmt.Errorf("query: scan daily session diagnostics: %w", err)
		}
		d.Codes, d.Gaps, d.AlertCodes = []string{}, []Gap{}, []string{}
		for _, dec := range []struct {
			raw string
			to  any
		}{{codes, &d.Codes}, {gaps, &d.Gaps}, {alerts, &d.AlertCodes}} {
			if err := json.Unmarshal([]byte(dec.raw), dec.to); err != nil {
				return nil, fmt.Errorf("query: decode daily session diagnostics of %s: %w", d.Date, err)
			}
		}
		d.FirstSnapshotAt, d.LastSnapshotAt = formatOutPtr(first), formatOutPtr(last)
		if jobsOK.Valid {
			n := int(jobsOK.Int64)
			d.JobsSucceeded = &n
		}
		if jobsFailed.Valid {
			n := int(jobsFailed.Int64)
			d.JobsFailed = &n
		}
		d.RecordedAt = formatOut(recorded)
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("query: daily session diagnostics: %w", err)
	}
	return out, nil
}

// IntradayDay is a trading day with snapshots that lacks part of a session.
type IntradayDay struct {
	Date  string   `json:"date"`
	Codes []string `json:"codes"`
	Gaps  []Gap    `json:"gaps"`
}

// Gaps is the verdict on each trading day of a span (issue #1054).
type Gaps struct {
	// WholeDay are days with no snapshot although earlier history exists
	// (health_today.missing_session_dates).
	WholeDay []string
	// Insufficient are days with no snapshot and no history before them (#814).
	Insufficient []string
	// Intraday are days with snapshots but an incomplete session.
	Intraday []IntradayDay
}

// Judge judges the TSE trading days (JST) of [from, to). A day the app recorded
// after its close is taken from that record, which outlives the snapshot and
// job retention; any other day (the app was not running to record it, e.g. a
// day just after a restart or before the record existed) is judged live from
// market_snapshots (sessiongap.Observe, read-only; nothing is written). All
// lists are empty, never nil, and in date order.
func Judge(ctx context.Context, q Q, from, to time.Time) (Gaps, error) {
	g := Gaps{WholeDay: []string{}, Insufficient: []string{}, Intraday: []IntradayDay{}}
	recorded, err := Read(ctx, q, from, to, MaxRows)
	if err != nil {
		return Gaps{}, err
	}
	byDate := make(map[string]Diagnostic, len(recorded))
	for _, d := range recorded {
		byDate[d.Date] = d
	}
	for day := from.In(jst); day.Before(to); day = day.AddDate(0, 0, 1) {
		if !marketcalendar.TSE.IsTradingDay(day) {
			continue
		}
		date := day.Format(time.DateOnly)
		if d, ok := byDate[date]; ok {
			g.add(date, sessiongap.Kind(d.GapKind), d.Codes, d.Gaps)
			continue
		}
		o, err := sessiongap.Observe(ctx, q, day)
		if err != nil {
			return Gaps{}, err
		}
		g.add(date, o.Kind, o.Codes, daily(o.Gaps))
	}
	return g, nil
}

func (g *Gaps) add(date string, kind sessiongap.Kind, codes []string, gaps []Gap) {
	switch kind {
	case sessiongap.KindWholeDay:
		g.WholeDay = append(g.WholeDay, date)
	case sessiongap.KindInsufficientHistory:
		g.Insufficient = append(g.Insufficient, date)
	case sessiongap.KindIntraday:
		g.Intraday = append(g.Intraday, IntradayDay{Date: date, Codes: nonNilStrings(codes), Gaps: gaps})
	}
}

func daily(gaps []sessiongap.Gap) []Gap {
	out := make([]Gap, 0, len(gaps))
	for _, g := range gaps {
		out = append(out, Gap{Code: g.Code, From: g.From.UTC().Format(time.RFC3339), To: g.To.UTC().Format(time.RFC3339), Minutes: g.Minutes})
	}
	return out
}

func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// Summary is collection_status.session_gaps (issue #1054): the
// session completeness of the period's trading days, read from the daily
// records the app keeps (session_diagnostics_daily). They outlive the
// succeeded-job retention (7 days), so a gap older than that can still be
// traced to its counts and the warning codes of the time, which the jobs and
// health_alerts tables no longer hold.
type Summary struct {
	// WholeDayDates, IntradayDates and InsufficientHistoryDates are the
	// recorded days of the period by gap_kind, oldest first; a day the app
	// did not record (before this record existed, or the app was off through
	// the settle time and a restart) is not in any of them.
	WholeDayDates            []string `json:"whole_day_dates"`
	IntradayDates            []string `json:"intraday_dates"`
	InsufficientHistoryDates []string `json:"insufficient_history_dates"`
	// Days are the recorded days of the period, oldest first (at most MaxRows).
	Days []Diagnostic `json:"days"`
	Note string       `json:"note"`
}

const collectionGapsNote = "Days are recorded by the app after the close (and at start for the previous trading day), " +
	"only from the snapshots as they were; a missed day is never filled in afterwards. A trading day without a record was not observed " +
	"(see missing_session_dates for days with no snapshot). Non-trading days are never listed. insufficient_history_dates are days " +
	"before the first snapshot ever stored (#814), not gaps."

// collectionGaps reads the daily records of r.
func Summarize(ctx context.Context, q Q, from, to time.Time) (Summary, error) {
	days, err := Read(ctx, q, from, to, MaxRows)
	if err != nil {
		return Summary{}, err
	}
	g := Summary{
		WholeDayDates: []string{}, IntradayDates: []string{}, InsufficientHistoryDates: []string{},
		Days: days, Note: collectionGapsNote,
	}
	for _, d := range days {
		switch sessiongap.Kind(d.GapKind) {
		case sessiongap.KindWholeDay:
			g.WholeDayDates = append(g.WholeDayDates, d.Date)
		case sessiongap.KindIntraday:
			g.IntradayDates = append(g.IntradayDates, d.Date)
		case sessiongap.KindInsufficientHistory:
			g.InsufficientHistoryDates = append(g.InsufficientHistoryDates, d.Date)
		}
	}
	return g, nil
}
