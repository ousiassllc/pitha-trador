package dailyguardreplay_test

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/dailyguardsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/dailyguardreplay"
)

var jst = dailyguardreplay.TradingDayZone

// at is 2026-10-<day> hh:mm JST.
func at(day, hour, minute int) time.Time {
	return time.Date(2026, 10, day, hour, minute, 0, 0, jst)
}

// trade is a position opened at oh:om and closed at ch:cm on 2026-10-14.
func trade(id int64, oh, om, ch, cm int, pnl float64) dailyguardreplay.Trade {
	return dailyguardreplay.Trade{ID: id, Symbol: "7203", OpenedAt: at(14, oh, om), ClosedAt: at(14, ch, cm), PnLJPY: pnl}
}

func only(t *testing.T, r dailyguardreplay.Report) dailyguardreplay.DayResult {
	t.Helper()
	if len(r.Days) != 1 {
		t.Fatalf("days = %d, want 1: %+v", len(r.Days), r.Days)
	}
	return r.Days[0]
}

func wantDay(t *testing.T, got, want dailyguardreplay.DayResult) {
	t.Helper()
	if got != want {
		t.Errorf("day =\n%+v\nwant\n%+v", got, want)
	}
}

func TestReplay_NoHaltDay_GuardChangesNothing(t *testing.T) {
	limits := dailyguardsettings.Limits{LossLimitJPY: 5000, ProfitLockTriggerJPY: 4000, ProfitLockGivebackPct: 50, MaxTrades: 5}
	r := dailyguardreplay.Replay([]dailyguardreplay.Trade{
		trade(1, 9, 0, 9, 30, 1000),
		trade(2, 10, 0, 10, 30, -500),
		trade(3, 11, 0, 11, 30, 800),
	}, limits)
	wantDay(t, only(t, r), dailyguardreplay.DayResult{
		Day: "2026-10-14", TradesAll: 3, TradesKept: 3,
		PnLWithoutJPY: 1300, PnLWithJPY: 1300,
		MaxDrawdownWithoutJPY: 500, MaxDrawdownWithJPY: 500,
	})
	if r.HaltedDays != 0 {
		t.Errorf("HaltedDays = %d", r.HaltedDays)
	}
}

func TestReplay_LossLimitDay_SkipsLaterEntries(t *testing.T) {
	r := dailyguardreplay.Replay([]dailyguardreplay.Trade{
		trade(1, 9, 0, 9, 30, -3000),
		trade(2, 9, 40, 10, 0, -2500), // day P&L -5,500 <= -5,000: halt at 10:00
		trade(3, 10, 30, 11, 0, 4000), // refused
		trade(4, 13, 0, 13, 30, -9000),
	}, dailyguardsettings.Limits{LossLimitJPY: 5000})
	wantDay(t, only(t, r), dailyguardreplay.DayResult{
		Day: "2026-10-14", TradesAll: 4, TradesKept: 2, TradesSkipped: 2,
		PnLWithoutJPY: -10500, PnLWithJPY: -5500, PnLSkippedJPY: -5000,
		MaxDrawdownWithoutJPY: 10500, MaxDrawdownWithJPY: 5500,
		HaltReason: domain.DailyHaltReasonLossLimit, HaltedAt: at(14, 10, 0),
	})
}

func TestReplay_LossLimitBoundary(t *testing.T) {
	limits := dailyguardsettings.Limits{LossLimitJPY: 5000}
	r := dailyguardreplay.Replay([]dailyguardreplay.Trade{trade(1, 9, 0, 9, 30, -4999), trade(2, 10, 0, 10, 30, 0)}, limits)
	if d := only(t, r); d.TradesSkipped != 0 || d.HaltReason != "" {
		t.Errorf("-4,999 must not halt: %+v", d)
	}
	r = dailyguardreplay.Replay([]dailyguardreplay.Trade{trade(1, 9, 0, 9, 30, -5000), trade(2, 10, 0, 10, 30, 0)}, limits)
	if d := only(t, r); d.TradesSkipped != 1 || d.HaltReason != domain.DailyHaltReasonLossLimit {
		t.Errorf("-5,000 must halt: %+v", d)
	}
}

func TestReplay_ProfitLockDay_StopsAfterGivebackFromPeak(t *testing.T) {
	r := dailyguardreplay.Replay([]dailyguardreplay.Trade{
		trade(1, 9, 0, 9, 30, 3000),    // peak 3,000 >= trigger 2,000
		trade(2, 10, 0, 10, 30, -1500), // 1,500 <= 3,000 - 50%: halt
		trade(3, 11, 0, 11, 30, -2000), // refused
	}, dailyguardsettings.Limits{ProfitLockTriggerJPY: 2000, ProfitLockGivebackPct: 50})
	wantDay(t, only(t, r), dailyguardreplay.DayResult{
		Day: "2026-10-14", TradesAll: 3, TradesKept: 2, TradesSkipped: 1,
		PnLWithoutJPY: -500, PnLWithJPY: 1500, PnLSkippedJPY: -2000,
		MaxDrawdownWithoutJPY: 3500, MaxDrawdownWithJPY: 1500,
		HaltReason: domain.DailyHaltReasonProfitLock, HaltedAt: at(14, 10, 30),
	})
}

func TestReplay_ProfitLockNotArmedBelowTrigger(t *testing.T) {
	r := dailyguardreplay.Replay([]dailyguardreplay.Trade{
		trade(1, 9, 0, 9, 30, 1900),
		trade(2, 10, 0, 10, 30, -1900),
		trade(3, 11, 0, 11, 30, 100),
	}, dailyguardsettings.Limits{ProfitLockTriggerJPY: 2000, ProfitLockGivebackPct: 50})
	if d := only(t, r); d.TradesSkipped != 0 || d.HaltReason != "" {
		t.Errorf("peak 1,900 < trigger must not arm the lock: %+v", d)
	}
}

func TestReplay_MaxTradesDay_RefusesTheNextEntry(t *testing.T) {
	r := dailyguardreplay.Replay([]dailyguardreplay.Trade{
		trade(1, 9, 0, 9, 10, 100),
		trade(2, 9, 20, 9, 30, -200),
		trade(3, 9, 40, 9, 50, 700), // third entry of a 2-trade day
	}, dailyguardsettings.Limits{MaxTrades: 2})
	wantDay(t, only(t, r), dailyguardreplay.DayResult{
		Day: "2026-10-14", TradesAll: 3, TradesKept: 2, TradesSkipped: 1,
		PnLWithoutJPY: 600, PnLWithJPY: -100, PnLSkippedJPY: 700,
		MaxDrawdownWithoutJPY: 200, MaxDrawdownWithJPY: 200,
		HaltReason: domain.DailyHaltReasonMaxTrades, HaltedAt: at(14, 9, 20),
	})
}

func TestReplay_MaxTradesCountsPositionsOpenedNotClosed(t *testing.T) {
	// Both positions are open at once; the third entry comes while they are still open.
	r := dailyguardreplay.Replay([]dailyguardreplay.Trade{
		trade(1, 9, 0, 14, 0, 100),
		trade(2, 9, 5, 14, 0, 100),
		trade(3, 9, 10, 14, 0, 100),
	}, dailyguardsettings.Limits{MaxTrades: 2})
	if d := only(t, r); d.TradesKept != 2 || d.TradesSkipped != 1 || d.PnLWithJPY != 200 || d.PnLWithoutJPY != 300 {
		t.Errorf("day = %+v", d)
	}
}

func TestReplay_Halt_LatchesEvenWhenKeptPositionsRecover(t *testing.T) {
	r := dailyguardreplay.Replay([]dailyguardreplay.Trade{
		trade(1, 9, 0, 9, 10, -6000), // halt at 9:10
		trade(2, 9, 1, 9, 20, 8000),  // already open: kept, closes in profit (+2,000)
		trade(3, 9, 30, 9, 40, 1000), // refused although the day is now positive
	}, dailyguardsettings.Limits{LossLimitJPY: 5000})
	d := only(t, r)
	if d.TradesKept != 2 || d.TradesSkipped != 1 || d.PnLWithJPY != 2000 || d.HaltReason != domain.DailyHaltReasonLossLimit {
		t.Errorf("day = %+v", d)
	}
	if !d.HaltedAt.Equal(at(14, 9, 10)) {
		t.Errorf("HaltedAt = %v, want 09:10 (the latch time must not move)", d.HaltedAt)
	}
}

func TestReplay_FirstRuleReachedKeepsTheHaltReason(t *testing.T) {
	// The trade count is reached first (9:00 open of the 1st trade, limit 1); a later loss does not change the reason.
	r := dailyguardreplay.Replay([]dailyguardreplay.Trade{trade(1, 9, 0, 9, 30, -9000)}, dailyguardsettings.Limits{LossLimitJPY: 5000, MaxTrades: 1})
	if d := only(t, r); d.HaltReason != domain.DailyHaltReasonMaxTrades {
		t.Errorf("HaltReason = %q, want max_daily_trades", d.HaltReason)
	}
}

func TestReplay_MultiDay_EachDayStartsFresh(t *testing.T) {
	limits := dailyguardsettings.Limits{LossLimitJPY: 5000, MaxTrades: 2}
	day2 := func(id int64, oh, ch int, pnl float64) dailyguardreplay.Trade {
		return dailyguardreplay.Trade{ID: id, OpenedAt: at(15, oh, 0), ClosedAt: at(15, ch, 0), PnLJPY: pnl}
	}
	r := dailyguardreplay.Replay([]dailyguardreplay.Trade{
		day2(10, 9, 10, 300),
		day2(11, 11, 12, -100),
		day2(12, 13, 14, 50), // 3rd of day 2: refused by max trades
		trade(1, 9, 0, 9, 30, -6000),
		trade(2, 10, 0, 10, 30, 500), // refused on day 1
	}, limits)
	if len(r.Days) != 2 || r.Days[0].Day != "2026-10-14" || r.Days[1].Day != "2026-10-15" {
		t.Fatalf("days = %+v, want 14th then 15th in date order", r.Days)
	}
	d1, d2 := r.Days[0], r.Days[1]
	if d1.HaltReason != domain.DailyHaltReasonLossLimit || d1.TradesSkipped != 1 {
		t.Errorf("day 1 = %+v", d1)
	}
	if d2.HaltReason != domain.DailyHaltReasonMaxTrades || d2.TradesKept != 2 || d2.TradesSkipped != 1 || d2.PnLWithJPY != 200 {
		t.Errorf("day 2 must not inherit day 1's halt: %+v", d2)
	}
	if r.HaltedDays != 2 {
		t.Errorf("HaltedDays = %d", r.HaltedDays)
	}
	tot := r.Total
	if tot.TradesAll != 5 || tot.TradesKept != 3 || tot.TradesSkipped != 2 ||
		tot.PnLWithoutJPY != -5250 || tot.PnLWithJPY != -5800 || tot.PnLSkippedJPY != 550 {
		t.Errorf("total = %+v", tot)
	}
	if tot.PnLWithoutJPY != tot.PnLWithJPY+tot.PnLSkippedJPY {
		t.Errorf("without (%v) != with (%v) + skipped (%v)", tot.PnLWithoutJPY, tot.PnLWithJPY, tot.PnLSkippedJPY)
	}
}

func TestReplay_TradingDayIsJST(t *testing.T) {
	// 2026-10-14 15:30 UTC is 2026-10-15 00:30 JST: the next trading day.
	opened := time.Date(2026, 10, 14, 15, 30, 0, 0, time.UTC)
	r := dailyguardreplay.Replay([]dailyguardreplay.Trade{{ID: 1, OpenedAt: opened, ClosedAt: opened.Add(time.Hour), PnLJPY: 10}}, dailyguardsettings.Limits{})
	if d := only(t, r); d.Day != "2026-10-15" {
		t.Errorf("Day = %s, want 2026-10-15", d.Day)
	}
}

func TestReplay_AllRulesDisabled_KeepsEverything(t *testing.T) {
	r := dailyguardreplay.Replay([]dailyguardreplay.Trade{trade(1, 9, 0, 9, 30, -99999), trade(2, 10, 0, 10, 30, -99999)}, dailyguardsettings.Limits{})
	if d := only(t, r); d.TradesSkipped != 0 || d.PnLWithJPY != d.PnLWithoutJPY || d.HaltReason != "" {
		t.Errorf("day = %+v", d)
	}
}

func TestReplay_EmptyInput(t *testing.T) {
	r := dailyguardreplay.Replay(nil, dailyguardsettings.Limits{LossLimitJPY: 1})
	if len(r.Days) != 0 || r.Total != (dailyguardreplay.DayResult{}) || r.HaltedDays != 0 {
		t.Fatalf("report = %+v, want empty", r)
	}
	var out bytes.Buffer
	if err := dailyguardreplay.Render(&out, r); err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(out.String(), "total") {
		t.Errorf("empty report still renders the totals row: %s", out.String())
	}
}

func TestReplay_MissingRealizedPnLCountsAsZero(t *testing.T) {
	missing := trade(1, 9, 0, 9, 30, 0)
	missing.PnLMissing = true
	r := dailyguardreplay.Replay([]dailyguardreplay.Trade{missing, trade(2, 10, 0, 10, 30, 100)}, dailyguardsettings.Limits{})
	if r.MissingPnL != 1 || only(t, r).PnLWithoutJPY != 100 {
		t.Errorf("report = %+v", r)
	}
}

func TestReplay_SameInstantClosesBeforeOpens(t *testing.T) {
	// Trade 1 closes at 10:00 into a loss halt; trade 2 opens at the same instant and is refused.
	r := dailyguardreplay.Replay([]dailyguardreplay.Trade{trade(2, 10, 0, 10, 30, 50), trade(1, 9, 0, 10, 0, -5000)}, dailyguardsettings.Limits{LossLimitJPY: 5000})
	if d := only(t, r); d.TradesSkipped != 1 {
		t.Errorf("day = %+v", d)
	}
}

func TestRender_ShowsLimitsLimitationsRowsAndPendingDecision(t *testing.T) {
	r := dailyguardreplay.Replay([]dailyguardreplay.Trade{
		trade(1, 9, 0, 9, 30, -6000),
		trade(2, 10, 0, 10, 30, 1500),
	}, dailyguardsettings.Limits{LossLimitJPY: 5000, ProfitLockTriggerJPY: 2000, ProfitLockGivebackPct: 50, MaxTrades: 3})
	var out bytes.Buffer
	if err := dailyguardreplay.Render(&out, r); err != nil {
		t.Fatalf("Render: %v", err)
	}
	text := out.String()
	for _, want := range []string{
		"損失上限 5,000 円", "利益確保 発動 2,000 円 / 戻し 50%", "最大取引回数 3 回",
		"含み損益は記録から復元できない", "決定待ち",
		"2026-10-14", "2/1/1", "-4,500", "-6,000", "+1,500", "daily_loss_limit_jpy", "total", "1 halted days",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("output lacks %q:\n%s", want, text)
		}
	}
}

func TestRender_DisabledRules(t *testing.T) {
	var out bytes.Buffer
	if err := dailyguardreplay.Render(&out, dailyguardreplay.Replay(nil, dailyguardsettings.Limits{})); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"損失上限 無効", "利益確保 無効", "最大取引回数 無効"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q: %s", want, out.String())
		}
	}
}
