package fxdiag

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"time"

	rfx "github.com/ousiassllc/pitha-trador/internal/domain/researchfx"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// Summarize reads the record of [r.From, r.To) (JST dates of the GMO trading
// day) per currency pair and returns it with the number of series. symbol, when
// not empty, limits the answer to that pair. now only decides which trading days
// are over (complete): the day now is in is still being collected and is neither
// recorded nor missing yet.
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

// lastCompleteDay is the latest trading day whose 06:00 JST rollover to the next
// day has passed at now.
func lastCompleteDay(now time.Time) string {
	today, _ := time.ParseInLocation(time.DateOnly, rfx.TradingDay(now), rfx.JST)
	return today.AddDate(0, 0, -1).Format(time.DateOnly)
}

// seriesSymbols are the pairs with a marker plus the configured ones, sorted, or
// just symbol when one is asked for.
func seriesSymbols(ctx context.Context, q query.Q, configured []string, only string) ([]string, error) {
	if only != "" {
		return []string{only}, nil
	}
	rows, err := q.QueryContext(ctx, `SELECT DISTINCT symbol FROM research_fx_days`)
	if err != nil {
		return nil, fmt.Errorf("fxdiag: list recorded pairs: %w", err)
	}
	defer func() { _ = rows.Close() }()
	set := slices.Clone(configured)
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, fmt.Errorf("fxdiag: scan recorded pair: %w", err)
		}
		if !slices.Contains(set, s) {
			set = append(set, s)
		}
	}
	slices.Sort(set)
	return set, rows.Err()
}

func summarizeSymbol(ctx context.Context, q query.Q, symbol, from, to, lastComplete string) (Series, error) {
	s := Series{Symbol: symbol}
	var first, last sql.NullString
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*), MIN(open_time), MAX(open_time), COALESCE(SUM(ask_open IS NULL), 0), COALESCE(SUM(bid_open IS NULL), 0)
FROM research_fx_candles WHERE symbol = ? AND trading_day >= ? AND trading_day < ?`, symbol, from, to).
		// A BID-only minute has a NULL ASK side, so the first sum counts the BID-only minutes.
		Scan(&s.Candles, &first, &last, &s.BidOnlyMinutes, &s.AskOnlyMinutes); err != nil {
		return s, fmt.Errorf("fxdiag: count candles of %s: %w", symbol, err)
	}
	if first.Valid {
		f, l := query.FormatOut(first.String), query.FormatOut(last.String)
		s.FirstOpenTime, s.LastOpenTime = &f, &l
	}
	days, err := summarizeDays(ctx, q, symbol, from, to, lastComplete)
	if err != nil {
		return s, err
	}
	s.Days = days
	s.InteriorGaps, err = interiorGaps(ctx, q, symbol, from, to)
	return s, err
}

// summarizeDays counts the markers in the period and the completed days since
// the series started that have none.
func summarizeDays(ctx context.Context, q query.Q, symbol, from, to, lastComplete string) (Days, error) {
	d := Days{ClosedDays: []string{}, MissingDays: []string{}}
	var firstMarked sql.NullString
	if err := q.QueryRowContext(ctx, `SELECT MIN(trading_day) FROM research_fx_days WHERE symbol = ?`, symbol).Scan(&firstMarked); err != nil {
		return d, fmt.Errorf("fxdiag: first marked day of %s: %w", symbol, err)
	}
	if firstMarked.Valid {
		d.FirstMarkedDay = &firstMarked.String
	}
	rows, err := q.QueryContext(ctx, `SELECT trading_day, status FROM research_fx_days WHERE symbol = ? AND trading_day >= ? AND trading_day < ? ORDER BY trading_day`, symbol, from, to)
	if err != nil {
		return d, fmt.Errorf("fxdiag: list days of %s: %w", symbol, err)
	}
	defer func() { _ = rows.Close() }()
	marked := map[string]bool{}
	for rows.Next() {
		var day, status string
		if err := rows.Scan(&day, &status); err != nil {
			return d, fmt.Errorf("fxdiag: scan day of %s: %w", symbol, err)
		}
		marked[day] = true
		through := day
		d.RecordedThrough = &through
		if status == rfx.StatusEmpty {
			d.MarketClosed++
			d.ClosedDays = append(d.ClosedDays, day)
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
	start, err := time.ParseInLocation(time.DateOnly, max(from, *d.FirstMarkedDay), rfx.JST)
	if err != nil {
		return d, fmt.Errorf("fxdiag: day %q: %w", from, err)
	}
	for day := start; ; day = day.AddDate(0, 0, 1) {
		name := day.Format(time.DateOnly)
		if name >= to || name > lastComplete {
			break
		}
		if !marked[name] {
			d.Missing++
			d.MissingDays = append(d.MissingDays, name)
		}
	}
	return d, nil
}

// gapSQL lists, per trading day, every pair of neighbouring candles more than one
// minute apart: prev_time and next_time are the candles around the gap.
const gapSQL = `
SELECT trading_day, prev_time, open_time,
	(CAST(strftime('%s', substr(open_time, 1, 19)) AS INTEGER) - CAST(strftime('%s', substr(prev_time, 1, 19)) AS INTEGER)) / 60 - 1 AS minutes
FROM (
	SELECT trading_day, open_time, LAG(open_time) OVER (PARTITION BY trading_day ORDER BY open_time) AS prev_time
	FROM research_fx_candles WHERE symbol = ? AND trading_day >= ? AND trading_day < ?
) WHERE prev_time IS NOT NULL AND minutes > 0 ORDER BY open_time`

// interiorGaps finds the runs of missing minutes between the first and the last
// candle of each trading day. A day's start and end are not gaps: a market that
// opens late or closes early on a day is not missing data.
func interiorGaps(ctx context.Context, q query.Q, symbol, from, to string) (InteriorGaps, error) {
	g := InteriorGaps{List: []GapRun{}}
	rows, err := q.QueryContext(ctx, gapSQL, symbol, from, to)
	if err != nil {
		return g, fmt.Errorf("fxdiag: gaps of %s: %w", symbol, err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var day, prev, next string
		var minutes int
		if err := rows.Scan(&day, &prev, &next, &minutes); err != nil {
			return g, fmt.Errorf("fxdiag: scan gap of %s: %w", symbol, err)
		}
		g.Runs++
		g.MissingMinutes += minutes
		if len(g.List) >= MaxGapRuns {
			g.Truncated = true
			continue
		}
		p, err := sqlutil.ParseTime(prev)
		if err != nil {
			return g, err
		}
		n, err := sqlutil.ParseTime(next)
		if err != nil {
			return g, err
		}
		g.List = append(g.List, GapRun{
			TradingDay: day, Start: p.Add(minute).UTC().Format(time.RFC3339), End: n.Add(-minute).UTC().Format(time.RFC3339), Minutes: minutes,
		})
	}
	return g, rows.Err()
}
