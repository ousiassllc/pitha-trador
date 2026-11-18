// Package researchbarsdiag is the read-only view of the 立花 research daily-bar
// collection (issue #1082) behind the research_daily_bars_status MCP tool: its
// state, what daily_bars holds, how far the backfill is, the latest runs and
// the bars per recent trade date. Every statement is a SELECT of named
// columns; the result structs list exactly the fields the API returns, and the
// free text of a run's reason is cut before the first ": " (the part after it
// is the broker's own wording). Nothing here is a trading input.
package researchbarsdiag

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// Description is the tool's description (docs/api/endpoints/paper-results-mcp-research-bars.md).
const Description = "立花の日足の研究用の収集（取引の入力ではない。ブローカーが kabu のままでも夜間に立花の日足を記録する）の読み取り専用の状況: " +
	"status（state: disabled / not_configured / broker_is_tachibana / waiting / running / ok / error / unknown、detail、updated_at。認証情報は返さない）、" +
	"daily_bars（daily_bars の銘柄数と最初・最後の trade_date。行数は表が大きく 3 秒の上限に収まらないため返さない。research_bars_saved は研究用の収集が書いた日足の累計）、" +
	"backfill（最新の研究用の夜の対象銘柄数 universe に対する、取得済みの銘柄数 backfilled と割合 percent、直近の試みが失敗した銘柄数 failed_symbols）、" +
	"runs（直近 10 夜の status・symbols・requests・saved_bars・failed・duration_ms・reason）、bars_per_day（直近 10 立会日の日足の本数）。期間の引数は無い"

const (
	// MaxRuns is how many nights runs lists.
	MaxRuns = 10
	// MaxDays is how many trade dates bars_per_day lists.
	MaxDays = 10
	// lookbackDays bounds the dates scanned for bars_per_day: ten trade dates
	// always fit in 30 calendar days (the longest holiday stretch is 10).
	lookbackDays = 30
	// maxReasonRunes is where a run's reason is cut.
	maxReasonRunes = query.MaxReasonRunes
)

// Status is research_tachibana_status of the bars component.
type Status struct {
	State     string  `json:"state"`
	Detail    string  `json:"detail"`
	UpdatedAt *string `json:"updated_at"`
}

// DailyBars summarizes daily_bars (whatever wrote it).
type DailyBars struct {
	Symbols        int     `json:"symbols"`
	FirstTradeDate *string `json:"first_trade_date"`
	LastTradeDate  *string `json:"last_trade_date"`
	// ResearchBarsSaved is the bars the research collection wrote, over all nights.
	ResearchBarsSaved int `json:"research_bars_saved"`
}

// Backfill is how far the symbols of the latest research night have their history.
type Backfill struct {
	Universe      int      `json:"universe"`
	Backfilled    int      `json:"backfilled"`
	Percent       *float64 `json:"percent"` // null while the universe is unknown
	FailedSymbols int      `json:"failed_symbols"`
}

// Run is one research night.
type Run struct {
	RunDate    string  `json:"run_date"`
	Status     string  `json:"status"`
	StartedAt  string  `json:"started_at"`
	FinishedAt *string `json:"finished_at"`
	Symbols    int     `json:"symbols"`
	Requests   int     `json:"requests"`
	SavedBars  int     `json:"saved_bars"`
	Failed     int     `json:"failed"`
	DurationMS int64   `json:"duration_ms"`
	Reason     string  `json:"reason"`
}

// DayBars is the bars stored for one trade date.
type DayBars struct {
	TradeDate string `json:"trade_date"`
	Bars      int    `json:"bars"`
}

// Data is the tool's result.
type Data struct {
	Status     Status    `json:"status"`
	DailyBars  DailyBars `json:"daily_bars"`
	Backfill   Backfill  `json:"backfill"`
	Runs       []Run     `json:"runs"`
	BarsPerDay []DayBars `json:"bars_per_day"`
}

// symbolsCTE lists the distinct symbols of daily_bars by index seeks on the
// primary key (symbol, trade_date), not a scan of the (very large) table.
const symbolsCTE = `WITH RECURSIVE s(sym) AS (
	SELECT MIN(symbol) FROM daily_bars
	UNION ALL
	SELECT (SELECT MIN(symbol) FROM daily_bars WHERE symbol > s.sym) FROM s WHERE s.sym IS NOT NULL
)`

// Read assembles the view (a Data). It returns it and the number of runs listed.
func Read(ctx context.Context, q query.Q) (any, int, error) {
	d := Data{Runs: []Run{}, BarsPerDay: []DayBars{}}
	steps := []func(context.Context, query.Q, *Data) error{readStatus, readDailyBars, readBackfill, readRuns, readBarsPerDay}
	for _, step := range steps {
		if err := step(ctx, q, &d); err != nil {
			return Data{}, 0, err
		}
	}
	return d, len(d.Runs), nil
}

func readStatus(ctx context.Context, q query.Q, d *Data) error {
	var state, detail, updated string
	err := q.QueryRowContext(ctx, `SELECT state, detail, updated_at FROM research_tachibana_status WHERE component = 'bars'`).
		Scan(&state, &detail, &updated)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		d.Status = Status{State: "unknown"}
		return nil
	case err != nil:
		return fmt.Errorf("researchbarsdiag: status: %w", err)
	}
	out := query.FormatOut(updated)
	d.Status = Status{State: state, Detail: reasonHead(detail), UpdatedAt: &out}
	return nil
}

func readDailyBars(ctx context.Context, q query.Q, d *Data) error {
	rows, err := q.QueryContext(ctx, symbolsCTE+` SELECT (SELECT MIN(trade_date) FROM daily_bars WHERE symbol = s.sym),
		(SELECT MAX(trade_date) FROM daily_bars WHERE symbol = s.sym) FROM s WHERE s.sym IS NOT NULL`)
	if err != nil {
		return fmt.Errorf("researchbarsdiag: daily_bars: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var first, last string
		if err := rows.Scan(&first, &last); err != nil {
			return fmt.Errorf("researchbarsdiag: scan daily_bars: %w", err)
		}
		d.DailyBars.Symbols++
		if d.DailyBars.FirstTradeDate == nil || first < *d.DailyBars.FirstTradeDate {
			d.DailyBars.FirstTradeDate = &first
		}
		if d.DailyBars.LastTradeDate == nil || last > *d.DailyBars.LastTradeDate {
			d.DailyBars.LastTradeDate = &last
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("researchbarsdiag: daily_bars: %w", err)
	}
	if err := q.QueryRowContext(ctx, `SELECT COALESCE(SUM(bars_saved), 0) FROM research_daily_bar_progress`).
		Scan(&d.DailyBars.ResearchBarsSaved); err != nil {
		return fmt.Errorf("researchbarsdiag: bars saved: %w", err)
	}
	return nil
}

func readBackfill(ctx context.Context, q query.Q, d *Data) error {
	err := q.QueryRowContext(ctx, `SELECT symbols FROM research_daily_bar_runs ORDER BY run_date DESC LIMIT 1`).Scan(&d.Backfill.Universe)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("researchbarsdiag: universe: %w", err)
	}
	err = q.QueryRowContext(ctx, `SELECT COUNT(first_ok_night), COALESCE(SUM(CASE WHEN last_ok = 0 THEN 1 ELSE 0 END), 0)
		FROM research_daily_bar_progress`).Scan(&d.Backfill.Backfilled, &d.Backfill.FailedSymbols)
	if err != nil {
		return fmt.Errorf("researchbarsdiag: backfill: %w", err)
	}
	if u := d.Backfill.Universe; u > 0 {
		// A symbol that left the universe may still be counted: never above 100.
		p := min(100, float64(d.Backfill.Backfilled)*1000/float64(u)/10)
		p = float64(int(p*10+0.5)) / 10
		d.Backfill.Percent = &p
	}
	return nil
}

func readRuns(ctx context.Context, q query.Q, d *Data) error {
	rows, err := q.QueryContext(ctx, `SELECT run_date, status, started_at, finished_at, symbols, requests, saved_bars,
		failed, duration_ms, error FROM research_daily_bar_runs ORDER BY run_date DESC LIMIT ?`, MaxRuns)
	if err != nil {
		return fmt.Errorf("researchbarsdiag: runs: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var (
			r        Run
			started  string
			finished sql.NullString
			reason   string
		)
		if err := rows.Scan(&r.RunDate, &r.Status, &started, &finished, &r.Symbols, &r.Requests, &r.SavedBars, &r.Failed, &r.DurationMS, &reason); err != nil {
			return fmt.Errorf("researchbarsdiag: scan runs: %w", err)
		}
		r.StartedAt = query.FormatOut(started)
		if finished.Valid {
			out := query.FormatOut(finished.String)
			r.FinishedAt = &out
		}
		r.Reason = reasonHead(reason)
		d.Runs = append(d.Runs, r)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("researchbarsdiag: runs: %w", err)
	}
	return nil
}

func readBarsPerDay(ctx context.Context, q query.Q, d *Data) error {
	if d.DailyBars.LastTradeDate == nil {
		return nil
	}
	last, err := time.Parse(time.DateOnly, *d.DailyBars.LastTradeDate)
	if err != nil {
		return fmt.Errorf("researchbarsdiag: last trade date: %w", err)
	}
	from := last.AddDate(0, 0, -lookbackDays).Format(time.DateOnly)
	rows, err := q.QueryContext(ctx, symbolsCTE+` SELECT b.trade_date, COUNT(*) FROM s JOIN daily_bars b
		ON b.symbol = s.sym AND b.trade_date >= ? GROUP BY b.trade_date ORDER BY b.trade_date DESC LIMIT ?`, from, MaxDays)
	if err != nil {
		return fmt.Errorf("researchbarsdiag: bars per day: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var day DayBars
		if err := rows.Scan(&day.TradeDate, &day.Bars); err != nil {
			return fmt.Errorf("researchbarsdiag: scan bars per day: %w", err)
		}
		d.BarsPerDay = append(d.BarsPerDay, day)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("researchbarsdiag: bars per day: %w", err)
	}
	return nil
}

// reasonHead is text up to the first ": ", cut to maxReasonRunes: what follows
// the colon in a stored reason is the broker's or a driver's own wording.
func reasonHead(text string) string {
	if i := strings.Index(text, ": "); i >= 0 {
		text = text[:i]
	}
	if r := []rune(text); len(r) > maxReasonRunes {
		return string(r[:maxReasonRunes])
	}
	return text
}
