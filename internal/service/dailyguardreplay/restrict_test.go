package dailyguardreplay_test

import (
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/dailyguardsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/dailyguardreplay"
)

func TestReplay_StillOpenTradeCountsAsOpenedWithoutPnL(t *testing.T) {
	r := dailyguardreplay.Replay([]dailyguardreplay.Trade{
		trade(1, 9, 0, 9, 30, 700),
		{ID: 2, Symbol: "7203", OpenedAt: at(14, 10, 0)}, // no ClosedAt: still open
	}, dailyguardsettings.Limits{})
	wantDay(t, only(t, r), dailyguardreplay.DayResult{
		Day: "2026-10-14", TradesAll: 2, TradesKept: 2,
		PnLWithoutJPY: 700, PnLWithJPY: 700,
	})
}

func TestReport_Restrict_KeepsOnlyTheRequestedDaysAndResums(t *testing.T) {
	overnight := dailyguardreplay.Trade{ID: 1, Symbol: "7203", OpenedAt: at(13, 14, 0), ClosedAt: at(14, 9, 10), PnLJPY: 300}
	crossesTo := dailyguardreplay.Trade{ID: 2, Symbol: "7203", OpenedAt: at(14, 11, 0), ClosedAt: at(15, 9, 30), PnLJPY: -9999, PnLMissing: true}
	r := dailyguardreplay.Replay([]dailyguardreplay.Trade{overnight, crossesTo}, dailyguardsettings.Limits{})
	if len(r.Days) != 3 || r.MissingPnL != 1 {
		t.Fatalf("unrestricted days = %d, missing = %d, want 3 and 1", len(r.Days), r.MissingPnL)
	}

	got := r.Restrict(at(14, 0, 0), at(15, 0, 0))
	d := only(t, got)
	if d.Day != "2026-10-14" || d.TradesAll != 1 || d.PnLWithoutJPY != 300 {
		t.Errorf("restricted day = %+v, want 2026-10-14 with 1 opened trade and +300 (the overnight close)", d)
	}
	if got.Total.PnLWithoutJPY != 300 || got.Total.TradesAll != 1 || got.MissingPnL != 0 {
		t.Errorf("totals = %+v missing %d, want them re-summed over the restricted day only", got.Total, got.MissingPnL)
	}
}
