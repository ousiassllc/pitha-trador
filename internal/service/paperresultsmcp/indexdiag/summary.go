package indexdiag

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"time"

	rindex "github.com/ousiassllc/pitha-trador/internal/domain/researchindex"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
	"github.com/ousiassllc/pitha-trador/internal/service/researchindex"
)

// Summarize reads the record of [r.From, r.To) (JST dates of the trading day) per
// index code and returns it with the number of series. symbol, when not empty,
// limits the answer to that code. now only decides which trading days are over
// (complete): a day is complete once researchindex.CompleteAfter has passed.
func Summarize(ctx context.Context, q query.Q, r query.Range, now time.Time, symbol string) (Data, int, error) {
	rec, err := loadRecording(ctx, q)
	if err != nil {
		return Data{}, 0, err
	}
	from, to := r.From.In(query.JST).Format(time.DateOnly), r.To.In(query.JST).Format(time.DateOnly)
	symbols, err := seriesSymbols(ctx, q, rec.Symbols, symbol)
	if err != nil {
		return Data{}, 0, err
	}
	out := Data{From: from, To: to, Notice: Notice, Recording: rec, Series: []Series{}, Definitions: Definitions}
	lastComplete := lastCompleteDay(now)
	for _, s := range symbols {
		series, err := summarizeSymbol(ctx, q, s, from, to, lastComplete)
		if err != nil {
			return Data{}, 0, err
		}
		series.Configured = slices.Contains(rec.Symbols, s)
		out.Series = append(out.Series, series)
	}
	return out, len(out.Series), nil
}

// lastCompleteDay is the latest date whose session is complete at now: today from
// researchindex.CompleteAfter on, otherwise yesterday (a non-trading date is
// never counted anyway).
func lastCompleteDay(now time.Time) string {
	n := now.In(rindex.JST)
	if time.Duration(n.Hour())*time.Hour+time.Duration(n.Minute())*time.Minute >= researchindex.CompleteAfter {
		return n.Format(time.DateOnly)
	}
	return n.AddDate(0, 0, -1).Format(time.DateOnly)
}

// seriesSymbols are the codes with a marker plus the configured ones, sorted, or
// just symbol when one is asked for.
func seriesSymbols(ctx context.Context, q query.Q, configured []string, only string) ([]string, error) {
	if only != "" {
		return []string{only}, nil
	}
	rows, err := q.QueryContext(ctx, `SELECT DISTINCT symbol FROM research_index_days`)
	if err != nil {
		return nil, fmt.Errorf("indexdiag: list recorded codes: %w", err)
	}
	defer func() { _ = rows.Close() }()
	set := slices.Clone(configured)
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, fmt.Errorf("indexdiag: scan recorded code: %w", err)
		}
		if !slices.Contains(set, s) {
			set = append(set, s)
		}
	}
	slices.Sort(set)
	return set, rows.Err()
}

func summarizeSymbol(ctx context.Context, q query.Q, symbol, from, to, lastComplete string) (Series, error) {
	s := Series{Symbol: symbol, Name: rindex.Label(symbol)}
	var first, last sql.NullString
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*), MIN(minute_time), MAX(minute_time) FROM research_index_minutes WHERE symbol = ? AND trading_day >= ? AND trading_day < ?`,
		symbol, from, to).Scan(&s.Minutes, &first, &last); err != nil {
		return s, fmt.Errorf("indexdiag: count minutes of %s: %w", symbol, err)
	}
	if first.Valid {
		f, l := query.FormatOut(first.String), query.FormatOut(last.String)
		s.FirstOpenTime, s.LastOpenTime = &f, &l
	}
	start, err := rindex.DayStart(from)
	if err != nil {
		return s, fmt.Errorf("indexdiag: day %q: %w", from, err)
	}
	end, err := rindex.DayStart(to)
	if err != nil {
		return s, fmt.Errorf("indexdiag: day %q: %w", to, err)
	}
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM research_quote_samples WHERE source = 'tachibana' AND symbol = ? AND timestamp >= ? AND timestamp < ?`,
		symbol, sqlutil.FormatTime(start), sqlutil.FormatTime(end)).Scan(&s.SourceSamples); err != nil {
		return s, fmt.Errorf("indexdiag: count source samples of %s: %w", symbol, err)
	}
	if s.Days, err = summarizeDays(ctx, q, symbol, from, to, lastComplete); err != nil {
		return s, err
	}
	s.InteriorGaps, err = interiorGaps(ctx, q, symbol, from, to)
	return s, err
}

// summarizeDays counts the markers in the period and the completed trading days
// since the series started that have none.
func summarizeDays(ctx context.Context, q query.Q, symbol, from, to, lastComplete string) (Days, error) {
	d := Days{UnavailableDays: []string{}, MissingDays: []string{}}
	var firstMarked sql.NullString
	if err := q.QueryRowContext(ctx, `SELECT MIN(trading_day) FROM research_index_days WHERE symbol = ?`, symbol).Scan(&firstMarked); err != nil {
		return d, fmt.Errorf("indexdiag: first marked day of %s: %w", symbol, err)
	}
	if firstMarked.Valid {
		d.FirstMarkedDay = &firstMarked.String
	}
	rows, err := q.QueryContext(ctx, `SELECT trading_day, status FROM research_index_days WHERE symbol = ? AND trading_day >= ? AND trading_day < ? ORDER BY trading_day`, symbol, from, to)
	if err != nil {
		return d, fmt.Errorf("indexdiag: list days of %s: %w", symbol, err)
	}
	defer func() { _ = rows.Close() }()
	marked := map[string]bool{}
	for rows.Next() {
		var day, status string
		if err := rows.Scan(&day, &status); err != nil {
			return d, fmt.Errorf("indexdiag: scan day of %s: %w", symbol, err)
		}
		marked[day] = true
		through := day
		d.RecordedThrough = &through
		if status == rindex.StatusUnavailable {
			d.Unavailable++
			d.UnavailableDays = append(d.UnavailableDays, day)
		} else {
			d.Recorded++
		}
	}
	if err := rows.Err(); err != nil {
		return d, err
	}
	if d.FirstMarkedDay == nil {
		return d, nil
	}
	start, err := rindex.DayStart(max(from, *d.FirstMarkedDay))
	if err != nil {
		return d, fmt.Errorf("indexdiag: day %q: %w", from, err)
	}
	for day := start; ; day = day.AddDate(0, 0, 1) {
		name := day.Format(time.DateOnly)
		if name >= to || name > lastComplete {
			break
		}
		if marketcalendar.TSE.IsTradingDay(day) && !marked[name] {
			d.Missing++
			d.MissingDays = append(d.MissingDays, name)
		}
	}
	return d, nil
}

// gapSQL lists, per trading day, every pair of neighbouring values more than one
// minute apart: prev_time and open_time are the values around the gap.
const gapSQL = `
SELECT trading_day, prev_time, minute_time FROM (
	SELECT trading_day, minute_time, LAG(minute_time) OVER (PARTITION BY trading_day ORDER BY minute_time) AS prev_time
	FROM research_index_minutes WHERE symbol = ? AND trading_day >= ? AND trading_day < ?
) WHERE prev_time IS NOT NULL
	AND CAST(strftime('%s', substr(minute_time, 1, 19)) AS INTEGER) - CAST(strftime('%s', substr(prev_time, 1, 19)) AS INTEGER) > 60
ORDER BY minute_time`

// interiorGaps finds the runs of session minutes without a value between the first
// and the last value of each trading day. A day's start and end are not gaps, and
// the lunch break is not in session, so a pair of values on either side of it
// (11:29 and 12:30) is no gap.
func interiorGaps(ctx context.Context, q query.Q, symbol, from, to string) (InteriorGaps, error) {
	g := InteriorGaps{List: []GapRun{}}
	rows, err := q.QueryContext(ctx, gapSQL, symbol, from, to)
	if err != nil {
		return g, fmt.Errorf("indexdiag: gaps of %s: %w", symbol, err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var day, prev, next string
		if err := rows.Scan(&day, &prev, &next); err != nil {
			return g, fmt.Errorf("indexdiag: scan gap of %s: %w", symbol, err)
		}
		p, err := sqlutil.ParseTime(prev)
		if err != nil {
			return g, err
		}
		n, err := sqlutil.ParseTime(next)
		if err != nil {
			return g, err
		}
		for _, run := range sessionRuns(p, n) {
			g.Runs++
			g.MissingMinutes += run.Minutes
			if len(g.List) >= MaxGapRuns {
				g.Truncated = true
				continue
			}
			run.TradingDay = day
			g.List = append(g.List, run)
		}
	}
	return g, rows.Err()
}

// sessionRuns are the runs of session minutes strictly between prev and next, in
// order: the lunch break splits nothing and counts nothing.
func sessionRuns(prev, next time.Time) []GapRun {
	var runs []GapRun
	var cur *GapRun
	for t := prev.Add(minute); t.Before(next); t = t.Add(minute) {
		if !marketcalendar.TSE.IsOpen(t) {
			cur = nil
			continue
		}
		if cur == nil {
			runs = append(runs, GapRun{Start: t.UTC().Format(time.RFC3339)})
			cur = &runs[len(runs)-1]
		}
		cur.End, cur.Minutes = t.UTC().Format(time.RFC3339), cur.Minutes+1
	}
	return runs
}
