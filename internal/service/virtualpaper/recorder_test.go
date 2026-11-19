package virtualpaper_test

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	repo "github.com/ousiassllc/pitha-trador/internal/repository/virtualpaper"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest/variant"
	"github.com/ousiassllc/pitha-trador/internal/service/virtualpaper"
)

// expected replays params over the synthetic day directly through the engine:
// what the profile's recorded trades must equal.
func expected(t *testing.T, day string, params variant.Params) []backtest.Trade {
	t.Helper()
	start := at(day, 0, 0)
	trades, err := backtest.ShadowBacktestPortfolioTrades(context.Background(), params.Apply(configsOf(day, dayPrices)), backtest.Period{Start: start, End: start.AddDate(0, 0, 1)})
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	return trades
}

func recorded(t *testing.T, db *sql.DB, id int64, day string) []repo.Trade {
	t.Helper()
	rows, err := db.Query(`SELECT instrument_id, symbol, direction, entry_at, entry_price, exit_at, exit_price, exit_reason, quantity,
gross_return_pct, slippage_return_pct, fee_return_pct, net_return_pct, pnl_jpy FROM virtual_paper_trades WHERE profile_id = ? AND day = ? ORDER BY entry_at`, id, day)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []repo.Trade
	for rows.Next() {
		var (
			tr          repo.Trade
			entry, exit string
		)
		if err := rows.Scan(&tr.InstrumentID, &tr.Symbol, &tr.Direction, &entry, &tr.EntryPrice, &exit, &tr.ExitPrice, &tr.ExitReason, &tr.Quantity,
			&tr.Gross, &tr.Slippage, &tr.Fee, &tr.Net, &tr.PnLJPY); err != nil {
			t.Fatal(err)
		}
		out = append(out, tr)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// Per profile, the virtual trades are exactly what that profile's settings make
// the replay do over the same decisions and bars, and profiles with different
// settings differ as their settings imply.
func TestRun_RecordsEachProfileExactlyPerItsParams(t *testing.T) {
	db := openDB(t)
	created := at("2026-01-05", 12, 0)
	profiles := map[string]variant.Params{
		"baseline":    {},
		"early_tp":    {TakeProfitPct: ptr(1.0)},
		"strict":      {MinEntryQualityLong: ptr(domain.JevEntryQualityExceptional)},
		"no_exit":     {TakeProfitPct: ptr(5.0), TrailingStopPct: ptr(0.0)},
		"cheap_costs": {SlippageBps: ptr(0.0), FeeBps: ptr(0.0)},
	}
	ids := map[string]int64{}
	for name, p := range profiles {
		ids[name] = create(t, db, name, created, p)
	}
	src := &daySource{}
	runAll(t, recorder(db, src, at("2026-01-07", 16, 30)))

	const day = "2026-01-06"
	for name, params := range profiles {
		want := expected(t, day, params)
		got := recorded(t, db, ids[name], day)
		if len(got) != len(want) {
			t.Fatalf("%s: %d trades recorded, want %d", name, len(got), len(want))
		}
		for i, w := range want {
			g := got[i]
			if g.Symbol != w.Symbol || g.Direction != w.Direction || g.ExitReason != w.ExitReason || g.Quantity != w.Quantity ||
				g.EntryPrice != w.EntryPrice || g.ExitPrice != w.ExitPrice || g.Net != w.NetReturnPct || g.Gross != w.GrossReturnPct ||
				g.Slippage != w.SlippageReturnPct || g.Fee != w.FeeReturnPct {
				t.Errorf("%s: trade %d = %+v, want the engine's %+v", name, i, g, w)
			}
			if wantPnL := w.NetReturnPct * float64(w.Quantity) * w.EntryPrice / 100; math.Abs(g.PnLJPY-wantPnL) > 1e-6 || g.Quantity == 0 {
				t.Errorf("%s: pnl_jpy = %v (quantity %d), want %v", name, g.PnLJPY, g.Quantity, wantPnL)
			}
		}
	}
	// The settings make the replays differ as they imply.
	reason := func(name string) string {
		t.Helper()
		got := recorded(t, db, ids[name], day)
		if len(got) != 1 {
			t.Fatalf("%s: %d trades, want 1", name, len(got))
		}
		return got[0].ExitReason
	}
	if got := reason("baseline"); got != domain.ExitReasonTrailingStop {
		t.Errorf("baseline exit = %q, want trailing_stop", got)
	}
	if got := reason("early_tp"); got != domain.ExitReasonTakeProfit {
		t.Errorf("early_tp exit = %q, want take_profit", got)
	}
	if got := reason("no_exit"); got != backtest.ExitReasonDataEnded {
		t.Errorf("no_exit exit = %q, want data_ended", got)
	}
	if got := recorded(t, db, ids["strict"], day); len(got) != 0 {
		t.Errorf("strict recorded %d trades, want 0: its entry quality threshold rejects the decision", len(got))
	}
	base, cheap := recorded(t, db, ids["baseline"], day)[0], recorded(t, db, ids["cheap_costs"], day)[0]
	if cheap.Net <= base.Net {
		t.Errorf("zero slippage/fee net %v <= baseline net %v", cheap.Net, base.Net)
	}
	// Every profile saw the same decisions and bars: the day marks agree.
	for name := range profiles {
		var bars, decisions int
		if err := db.QueryRow(`SELECT bars, decisions_seen FROM virtual_paper_days WHERE profile_id = ? AND day = ?`, ids[name], day).Scan(&bars, &decisions); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if bars != len(dayPrices) || decisions != 1 {
			t.Errorf("%s: bars=%d decisions_seen=%d, want %d and 1", name, bars, decisions, len(dayPrices))
		}
	}
	var unfinished int
	if err := db.QueryRow(`SELECT unfinished FROM virtual_paper_days WHERE profile_id = ? AND day = ?`, ids["no_exit"], day).Scan(&unfinished); err != nil || unfinished != 1 {
		t.Errorf("no_exit unfinished = %d (%v), want 1", unfinished, err)
	}
}

// A day is replayed again after a crash that left no mark: the trades are not
// duplicated, and an already marked day is not replayed at all.
func TestRun_ReplayingADayAgainDuplicatesNothing(t *testing.T) {
	db := openDB(t)
	id := create(t, db, "p", at("2026-01-05", 12, 0), variant.Params{})
	src := &daySource{}
	now := at("2026-01-07", 16, 30)
	runAll(t, recorder(db, src, now))
	trades := count(t, db, `SELECT COUNT(*) FROM virtual_paper_trades`)
	if trades != 1 {
		t.Fatalf("%d trades after the first run, want 1", trades)
	}
	callsBefore := len(src.calls)
	runAll(t, recorder(db, src, now))
	if len(src.calls) != callsBefore {
		t.Errorf("a marked day was loaded again: calls %v", src.calls)
	}

	// The crash: the trades are stored but the mark is not.
	if _, err := db.Exec(`DELETE FROM virtual_paper_days WHERE profile_id = ?`, id); err != nil {
		t.Fatal(err)
	}
	days := runAll(t, recorder(db, src, now))
	if len(days) == 0 {
		t.Fatal("the unmarked day was not replayed")
	}
	if got := count(t, db, `SELECT COUNT(*) FROM virtual_paper_trades`); got != trades {
		t.Errorf("%d trades after the replay, want %d (UNIQUE profile/instrument/entry)", got, trades)
	}
	if got := count(t, db, `SELECT COUNT(*) FROM virtual_paper_days WHERE profile_id = ? AND status = 'done'`, id); got != 1 {
		t.Errorf("%d done days, want 1", got)
	}
}

// One chunk is one JST day; the day the profile was created on is not replayed;
// the backlog is bounded to the most recent BacklogDays complete days; a day
// without bars is marked empty.
func TestRun_ChunksOneDayAndBoundsTheBacklog(t *testing.T) {
	db := openDB(t)
	create(t, db, "p", at("2026-01-01", 12, 0), variant.Params{})
	src := &daySource{empty: map[string]bool{"2026-03-04": true}}
	now := at("2026-03-05", 16, 30)
	r := recorder(db, src, now)

	res, err := r.Run(context.Background())
	if err != nil || !res.More || res.Day != "2026-02-23" {
		t.Fatalf("first chunk = %+v, %v; want the oldest day of the 10-day backlog, with more to do", res, err)
	}
	days := append([]string{res.Day}, runAll(t, r)...)
	if len(days) != virtualpaperBacklog {
		t.Fatalf("replayed %v, want %d days", days, virtualpaperBacklog)
	}
	if days[len(days)-1] != "2026-03-04" {
		t.Errorf("the last day = %s, want yesterday", days[len(days)-1])
	}
	var status string
	if err := db.QueryRow(`SELECT status FROM virtual_paper_days WHERE day = '2026-03-04'`).Scan(&status); err != nil || status != repo.DayEmpty {
		t.Errorf("a day without bars: status %q (%v), want empty", status, err)
	}
	if got := count(t, db, `SELECT COUNT(*) FROM virtual_paper_days WHERE day < '2026-02-23' OR day >= '2026-03-05'`); got != 0 {
		t.Errorf("%d marks outside the backlog window", got)
	}
}

const virtualpaperBacklog = 10

// A profile never gets the day it was created on, nor days before it (so two
// profiles created on different days are compared over their common days only).
func TestRun_ProfileStartsTheDayAfterItsCreation(t *testing.T) {
	db := openDB(t)
	create(t, db, "a", at("2026-01-05", 23, 59), variant.Params{})
	src := &daySource{}
	runAll(t, recorder(db, src, at("2026-01-08", 16, 30)))
	if got := src.calls; len(got) != 2 || got[0] != "2026-01-06" || got[1] != "2026-01-07" {
		t.Errorf("replayed days %v, want 2026-01-06 and 2026-01-07", got)
	}
	// Before 16:00 JST the day is not over for the caller: today is never replayed.
	if got := count(t, db, `SELECT COUNT(*) FROM virtual_paper_days WHERE day >= '2026-01-08'`); got != 0 {
		t.Errorf("today was replayed: %d marks", got)
	}
}

// A replay that fails is marked failed once and reported; the next run does not
// loop on it. A source that cannot load returns the error without marking.
func TestRun_FailuresAreReturnedNotPanicked(t *testing.T) {
	db := openDB(t)
	id := create(t, db, "p", at("2026-01-05", 12, 0), variant.Params{})
	r := recorder(db, &daySource{err: errors.New("source down")}, at("2026-01-07", 16, 30))
	if _, err := r.Run(context.Background()); err == nil {
		t.Fatal("a source failure was swallowed")
	}
	if got := count(t, db, `SELECT COUNT(*) FROM virtual_paper_days`); got != 0 {
		t.Errorf("%d marks after a load failure, want 0 (the day is retried)", got)
	}

	// Bars that fail the look-ahead check make the replay itself fail.
	res, err := recorder(db, brokenReplay{}, at("2026-01-07", 16, 30)).Run(context.Background())
	if err == nil || res.Day != "2026-01-06" {
		t.Fatalf("Run = %+v, %v; want the day's replay failure returned", res, err)
	}
	var status, msg string
	if e := db.QueryRow(`SELECT status, error FROM virtual_paper_days WHERE profile_id = ? AND day = '2026-01-06'`, id).Scan(&status, &msg); e != nil || status != repo.DayFailed || msg == "" {
		t.Errorf("mark = %q %q (%v), want failed with a reason", status, msg, e)
	}
	if got := count(t, db, `SELECT COUNT(*) FROM virtual_paper_trades`); got != 0 {
		t.Errorf("%d trades from a failed replay, want 0", got)
	}
	res, err = recorder(db, brokenReplay{}, at("2026-01-07", 16, 30)).Run(context.Background())
	if err != nil || res.Day != "" {
		t.Errorf("the failed day was tried again: %+v, %v", res, err)
	}
}

// brokenReplay returns configs whose bars carry a Feature that fails FR-BT-3.
type brokenReplay struct{}

func (brokenReplay) RunConfigs(_ context.Context, p backtest.Period) ([]backtest.RunConfig, error) {
	cfgs := configsOf(p.Start.In(jst).Format(time.DateOnly), dayPrices)
	cfgs[0].Snapshots[5].Feature.Return1m = new(0.5)
	return cfgs, nil
}

// slowSource blocks until its context ends, like a source that is far too slow.
type slowSource struct{}

func (slowSource) RunConfigs(ctx context.Context, _ backtest.Period) ([]backtest.RunConfig, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

// A slow replay is cut at the chunk budget: Run returns an error (the maintenance
// runner retries it later) instead of holding the lock, and marks nothing.
func TestRun_SlowSourceIsCutAtTheBudget(t *testing.T) {
	db := openDB(t)
	create(t, db, "p", at("2026-01-05", 12, 0), variant.Params{})
	r := virtualpaper.NewRecorder(db, slowSource{}).WithClock(func() time.Time { return at("2026-01-07", 16, 30) }).WithBudget(20 * time.Millisecond)
	if _, err := r.Run(context.Background()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run = %v, want the budget's deadline error", err)
	}
	if got := count(t, db, `SELECT COUNT(*) FROM virtual_paper_days`); got != 0 {
		t.Errorf("%d marks after a timed-out chunk, want 0", got)
	}
}
