package actuals

import (
	"math"
	"testing"
	"time"

	repo "github.com/ousiassllc/pitha-trador/internal/repository/actuals"
)

func TestMaxDrawdown(t *testing.T) {
	cases := []struct {
		name string
		pnl  []float64
		want float64
	}{
		{"empty", nil, 0},
		{"only gains", []float64{10, 20}, 0},
		{"loss before any profit is a drawdown from the start", []float64{-100, 50}, 100},
		{"peak then two falls", []float64{100, -30, -20, 60, -10}, 50},
		{"recovery above peak resets it", []float64{100, -40, 80, -30}, 40},
	}
	for _, c := range cases {
		if got := maxDrawdown(c.pnl); got != c.want {
			t.Errorf("%s: maxDrawdown(%v) = %v, want %v", c.name, c.pnl, got, c.want)
		}
	}
}

func TestProfitFactor(t *testing.T) {
	if v, ok := profitFactor([]float64{300, -100, -100}); !ok || v != 1.5 {
		t.Errorf("profitFactor = %v, %v, want 1.5", v, ok)
	}
	if _, ok := profitFactor([]float64{100, 0}); ok {
		t.Error("profitFactor without a loss must be undefined")
	}
}

func TestSlippageCosts(t *testing.T) {
	exit := 1010.0
	bps := func(v float64) *float64 { return &v }
	// SHORT: sells to enter at 1000 (5 bps worse: ref = 1000/(1-0.0005)), buys back at 1010 (10 bps worse).
	trade := repo.Trade{Side: "SHORT", Quantity: 100, EntryPrice: 1000, ExitPrice: &exit, EntrySlippageBps: bps(5), ExitSlippageBps: bps(10)}
	entry, ex := slippageCosts(trade)
	wantEntry := 100 * (1000/(1-0.0005) - 1000)
	wantExit := 100 * (1010 - 1010/(1+0.0010))
	if entry.recorded != 1 || ex.recorded != 1 || entry.jpy < wantEntry-1e-9 || entry.jpy > wantEntry+1e-9 || ex.jpy < wantExit-1e-9 || ex.jpy > wantExit+1e-9 {
		t.Errorf("slippage = %+v / %+v, want %v / %v", entry, ex, wantEntry, wantExit)
	}

	// Price improvement (negative bps) is a negative cost; nothing recorded is 0.
	improved := repo.Trade{Side: "LONG", Quantity: 100, EntryPrice: 1000, EntrySlippageBps: bps(-10)}
	if e, _ := slippageCosts(improved); e.jpy >= 0 {
		t.Errorf("improvement cost = %v, want negative", e.jpy)
	}
	if e, x := slippageCosts(repo.Trade{Side: "LONG", Quantity: 100, EntryPrice: 1000}); e != (fillCost{}) || x != (fillCost{}) {
		t.Errorf("unrecorded slippage = %+v / %+v, want zero", e, x)
	}
}

func TestReturnBps(t *testing.T) {
	if got := returnBps(repo.Trade{EntryPrice: 1000, Quantity: 100, RealizedPnL: 580}); math.Abs(got-58) > 1e-9 {
		t.Errorf("returnBps = %v, want 58", got)
	}
	if got := returnBps(repo.Trade{EntryPrice: 0, Quantity: 100, RealizedPnL: 5}); got != 0 {
		t.Errorf("returnBps without notional = %v, want 0", got)
	}
}

func TestDayAndWeekKeys(t *testing.T) {
	// 15:00Z is midnight JST: the next calendar day.
	if a, b := jstDay(time.Date(2026, 10, 12, 14, 59, 0, 0, time.UTC)), jstDay(time.Date(2026, 10, 12, 15, 0, 0, 0, time.UTC)); b != a+1 {
		t.Errorf("jstDay across JST midnight: %d then %d, want consecutive days", a, b)
	}
	// 2026-12-31 20:00Z is 2027-01-01 05:00 JST, still ISO week 53 of 2026.
	tr := repo.Trade{ClosedAt: time.Date(2026, 12, 31, 20, 0, 0, 0, time.UTC)}
	if got, want := dayKey(tr), "2027-01-01"; got != want {
		t.Errorf("dayKey = %q, want %q", got, want)
	}
	if got, want := weekKey(tr), "2026-W53"; got != want {
		t.Errorf("weekKey = %q, want %q", got, want)
	}
}
