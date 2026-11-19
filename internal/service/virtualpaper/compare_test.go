package virtualpaper_test

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/backtest"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest/variant"
	"github.com/ousiassllc/pitha-trador/internal/service/virtualpaper"
)

// losingDay is a day whose price falls 1.5% after the entry: every profile with
// the live stop loss (1%) loses.
var losingDay = []float64{1000, 1000, 1000, 985, 985, 985, 985, 985, 985, 985}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

// Two profiles created on different days are compared over the days both
// completed; the excluded day is listed; the figures equal the engine's over
// those days; the data_ended trade is counted as unfinished and left out.
func TestCompare_OverTheCommonDaysOnly(t *testing.T) {
	db := openDB(t)
	a := create(t, db, "baseline", at("2026-01-05", 12, 0), variant.Params{})                                                  // days 6, 7, 8
	b := create(t, db, "early_tp", at("2026-01-06", 12, 0), variant.Params{TakeProfitPct: ptr(1.0)})                           // days 7, 8
	c := create(t, db, "no_exit", at("2026-01-06", 13, 0), variant.Params{TakeProfitPct: ptr(5.0), TrailingStopPct: ptr(0.0)}) // days 7, 8
	src := &daySource{prices: map[string][]float64{"2026-01-08": losingDay}}
	runAll(t, recorder(db, src, at("2026-01-09", 16, 30)))

	got, err := virtualpaper.Compare(context.Background(), db, "2026-01-05", "2026-01-10", []int64{a, b, c})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Days) != 2 || got.Days[0] != "2026-01-07" || got.Days[1] != "2026-01-08" {
		t.Fatalf("days = %v, want the common 2026-01-07 and 2026-01-08", got.Days)
	}
	if len(got.ExcludedDays) != 1 || got.ExcludedDays[0].Day != "2026-01-06" || len(got.ExcludedDays[0].MissingProfileIDs) != 2 {
		t.Fatalf("excluded_days = %+v, want 2026-01-06 missing for the two later profiles", got.ExcludedDays)
	}
	if len(got.Profiles) != 3 || got.Profiles[0].ProfileID != a || got.Caution == "" {
		t.Fatalf("profiles = %+v", got.Profiles)
	}

	var want []backtest.Trade
	for _, day := range got.Days {
		prices := dayPrices
		if day == "2026-01-08" {
			prices = losingDay
		}
		start := at(day, 0, 0)
		trades, err := backtest.ShadowBacktestPortfolioTrades(context.Background(), variant.Params{}.Apply(configsOf(day, prices)), backtest.Period{Start: start, End: start.AddDate(0, 0, 1)})
		if err != nil {
			t.Fatal(err)
		}
		want = append(want, trades...)
	}
	m := backtest.Aggregate(want)
	base := got.Profiles[0]
	if base.TradeCount != 2 || !near(base.Expectancy.Value, m.Expectancy) || !near(base.MaxDrawdownPct, m.MaxDrawdownPct) || !near(base.WinRate, m.WinRate) {
		t.Errorf("baseline = %+v, want the engine's %+v over the common days", base, m)
	}
	if base.MaxDrawdownPct <= 0 || !base.Expectancy.HasCI || base.Expectancy.N != 2 || base.Unfinished != 0 {
		t.Errorf("baseline drawdown / interval = %+v: the losing day must draw down, two days give an interval", base)
	}
	if base.TotalPnLJPY == 0 || base.VsFirst != nil {
		t.Errorf("baseline pnl %v, vs_first %+v: it has pnl and is the reference", base.TotalPnLJPY, base.VsFirst)
	}

	early := got.Profiles[1]
	if early.VsFirst == nil || early.VsFirst.AgainstProfileID != a || early.VsFirst.DailyNetReturnPctDiff.N != 2 || !early.VsFirst.DailyNetReturnPctDiff.HasCI {
		t.Fatalf("early_tp paired = %+v, want the day-clustered difference against the first", early.VsFirst)
	}
	var sumA, sumB float64
	for _, tr := range want {
		sumA += tr.NetReturnPct
	}
	for _, day := range got.Days {
		prices := dayPrices
		if day == "2026-01-08" {
			prices = losingDay
		}
		start := at(day, 0, 0)
		trades, _ := backtest.ShadowBacktestPortfolioTrades(context.Background(), variant.Params{TakeProfitPct: ptr(1.0)}.Apply(configsOf(day, prices)), backtest.Period{Start: start, End: start.AddDate(0, 0, 1)})
		for _, tr := range trades {
			sumB += tr.NetReturnPct
		}
	}
	if !near(early.VsFirst.DailyNetReturnPctDiff.Value, (sumB-sumA)/2) {
		t.Errorf("paired difference = %v, want %v", early.VsFirst.DailyNetReturnPctDiff.Value, (sumB-sumA)/2)
	}

	// no_exit: its day-7 trade ended with the data (excluded), its day-8 trade hit the stop loss.
	open := got.Profiles[2]
	if open.Unfinished != 1 || open.TradeCount != 1 {
		t.Errorf("no_exit = %+v, want 1 finished trade and 1 unfinished one", open)
	}
}

func TestCompare_SelectionAndEmpty(t *testing.T) {
	db := openDB(t)
	ctx := context.Background()
	empty, err := virtualpaper.Compare(ctx, db, "2026-01-01", "2026-02-01", nil)
	if err != nil || len(empty.Profiles) != 0 || empty.Days == nil || empty.Profiles == nil || empty.ExcludedDays == nil {
		t.Fatalf("no data: %+v, %v; want empty (non-null) lists and no error", empty, err)
	}
	ids := make([]int64, 0, 6)
	for i := range 5 {
		ids = append(ids, create(t, db, string(rune('a'+i)), at("2026-01-05", 12, 0), variant.Params{}))
	}
	runAll(t, recorder(db, &daySource{}, at("2026-01-07", 16, 30)))

	all, err := virtualpaper.Compare(ctx, db, "2026-01-01", "2026-02-01", nil)
	if err != nil || len(all.Profiles) != 5 {
		t.Fatalf("default selection: %d profiles, %v; want all 5 that have data", len(all.Profiles), err)
	}
	var ie *virtualpaper.InvalidError
	for _, bad := range [][]int64{{ids[0]}, {ids[0], ids[0]}, {ids[0], 999}, append(append([]int64{}, ids...), 6)} {
		if _, err := virtualpaper.Compare(ctx, db, "2026-01-01", "2026-02-01", bad); !errors.As(err, &ie) || ie.Field != "profile_ids" {
			t.Errorf("profile_ids %v: %v, want an InvalidError on profile_ids", bad, err)
		}
	}
	// A sixth profile with data in the period makes the default selection too big.
	for _, id := range ids[:1] {
		if _, _, err := virtualpaper.Stop(ctx, db, at("2026-01-07", 17, 0), id); err != nil {
			t.Fatal(err)
		}
	}
	create(t, db, "f", at("2026-01-05", 12, 0), variant.Params{})
	runAll(t, recorder(db, &daySource{}, at("2026-01-07", 16, 30)))
	var le *virtualpaper.LimitError
	if _, err := virtualpaper.Compare(ctx, db, "2026-01-01", "2026-02-01", nil); !errors.As(err, &le) {
		t.Errorf("6 profiles with data: %v, want a LimitError asking for profile_ids", err)
	}
}
