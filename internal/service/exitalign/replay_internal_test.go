package exitalign

import (
	"math"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

var replayBase = time.Date(2026, 9, 28, 1, 0, 0, 0, time.UTC)

func bars(prices ...float64) []domain.Snapshot {
	out := make([]domain.Snapshot, len(prices))
	for i, p := range prices {
		out[i] = domain.Snapshot{Timestamp: replayBase.Add(time.Duration(i+1) * time.Minute), Price: p}
	}
	return out
}

var entryBar = domain.Snapshot{Timestamp: replayBase, Price: 1000}

// The shipped exit: SL 0.6 %, TP 1.2 %, trailing 0.5 %, 20 minutes.
var shippedRule = ExitConfig{StopLossPct: 0.6, TakeProfitPct: 1.2, TrailingStopPct: 0.5, MaxHoldingMinutes: 20}

func TestReplayExit(t *testing.T) {
	flatTail := func(n int, price float64) []float64 {
		out := make([]float64, n)
		for i := range out {
			out[i] = price
		}
		return out
	}
	for name, tc := range map[string]struct {
		direction string
		path      []domain.Snapshot
		rule      ExitConfig
		wantOK    bool
		wantNet   float64
		wantExit  string
	}{
		"take-profit at the bar price less cost": {
			direction: domain.JevDirectionLong, path: bars(1004, 1013), rule: shippedRule,
			wantOK: true, wantNet: 130 - 10, wantExit: domain.ExitReasonTakeProfit, // +1.3 % gross
		},
		"stop-loss": {
			direction: domain.JevDirectionLong, path: bars(998, 993), rule: shippedRule,
			wantOK: true, wantNet: -70 - 10, wantExit: domain.ExitReasonStopLoss,
		},
		"stop-loss before the trailing stop": {
			// 1003 -> 993 is both a 0.99 % retrace and below -0.6 %: SL has priority.
			direction: domain.JevDirectionLong, path: bars(1003, 993), rule: shippedRule,
			wantOK: true, wantNet: -70 - 10, wantExit: domain.ExitReasonStopLoss,
		},
		"trailing stop after a run-up": {
			// peak 1010 (+1.0 %), 1004 is a 0.59 % retrace from it, still +0.4 % from entry.
			direction: domain.JevDirectionLong, path: bars(1006, 1010, 1004), rule: shippedRule,
			wantOK: true, wantNet: 40 - 10, wantExit: domain.ExitReasonTrailingStop,
		},
		"SHORT: a falling price is a gain, a rising one hits the stop": {
			direction: domain.JevDirectionShort, path: bars(995, 987), rule: shippedRule,
			wantOK: true, wantNet: 130 - 10, wantExit: domain.ExitReasonTakeProfit,
		},
		"SHORT stop-loss": {
			direction: domain.JevDirectionShort, path: bars(1003, 1007), rule: shippedRule,
			wantOK: true, wantNet: -70 - 10, wantExit: domain.ExitReasonStopLoss,
		},
		"max holding at the first bar at or after it": {
			direction: domain.JevDirectionLong, path: bars(flatTail(21, 1001)...), rule: shippedRule,
			wantOK: true, wantNet: 0, // +10 bps gross less the 10 bps cost
			wantExit: domain.ExitReasonMaxHolding,
		},
		"bars end shortly before the max holding time": {
			direction: domain.JevDirectionLong, path: bars(flatTail(19, 1001)...), rule: shippedRule,
			wantOK: true, wantNet: 0, // +10 bps gross less the 10 bps cost
			wantExit: domain.ExitReasonMaxHolding,
		},
		"bars end well before the max holding time": {
			direction: domain.JevDirectionLong, path: bars(flatTail(10, 1001)...), rule: shippedRule,
			wantOK: false,
		},
		"a disabled rule never triggers": {
			direction: domain.JevDirectionLong, path: bars(1013, 987), rule: ExitConfig{MaxHoldingMinutes: 20},
			wantOK: false,
		},
	} {
		t.Run(name, func(t *testing.T) {
			got, ok := replayExit(entryBar, tc.path, 1000, tc.direction, tc.rule, 10)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v (got %+v)", ok, tc.wantOK, got)
			}
			if !ok {
				return
			}
			if got.Reason != tc.wantExit || math.Abs(got.NetBps-tc.wantNet) > 1e-9 {
				t.Fatalf("replay = %+v, want net %v by %s", got, tc.wantNet, tc.wantExit)
			}
		})
	}
}

// A price that crosses VWAP against the position exits on vwap_cross, and a
// price that starts on the adverse side does not (a cross, not a stay).
func TestReplayExit_VWAPCross(t *testing.T) {
	withVWAP := func(vwaps []float64, prices ...float64) []domain.Snapshot {
		b := bars(prices...)
		for i := range b {
			b[i].Feature.VWAP = vwaps[i]
		}
		return b
	}
	rule := ExitConfig{MaxHoldingMinutes: 20}
	entry := domain.Snapshot{Timestamp: replayBase, Price: 1000}
	entry.Feature.VWAP = 999

	// LONG above VWAP, then the price falls under it.
	got, ok := replayExit(entry, withVWAP([]float64{999, 1001}, 1002, 1000), 1000, domain.JevDirectionLong, rule, 0)
	if !ok || got.Reason != domain.ExitReasonVWAPCross || got.NetBps != 0 {
		t.Fatalf("replay = %+v, %v; want a vwap_cross exit at 0 bps", got, ok)
	}

	// LONG already below VWAP at entry and staying there: no cross.
	below := domain.Snapshot{Timestamp: replayBase, Price: 1000}
	below.Feature.VWAP = 1001
	if got, ok := replayExit(below, withVWAP([]float64{1001, 1001}, 1000, 999.5), 1000, domain.JevDirectionLong, rule, 0); ok {
		t.Fatalf("replay = %+v; want no exit while staying below VWAP", got)
	}
}

func TestSummarize(t *testing.T) {
	if got := summarize(nil); got != (DistributionBps{}) {
		t.Fatalf("summarize(nil) = %+v, want zero", got)
	}
	got := summarize([]float64{-40, -10, 0, 10, 50})
	want := DistributionBps{Count: 5, Mean: 2, StdDev: math.Sqrt((42*42 + 12*12 + 2*2 + 8*8 + 48*48) / 5.0), Min: -40, P5: -34, P25: -10, Median: 0, P75: 10, P95: 42, Max: 50}
	near := func(a, b float64) bool { return math.Abs(a-b) < 1e-9 }
	if got.Count != want.Count || !near(got.Mean, want.Mean) || !near(got.StdDev, want.StdDev) || !near(got.Min, want.Min) ||
		!near(got.P5, want.P5) || !near(got.P25, want.P25) || !near(got.Median, want.Median) ||
		!near(got.P75, want.P75) || !near(got.P95, want.P95) || !near(got.Max, want.Max) {
		t.Fatalf("summarize = %+v, want %+v", got, want)
	}
	if one := summarize([]float64{7}); one.Median != 7 || one.P95 != 7 || one.StdDev != 0 {
		t.Fatalf("summarize of one value = %+v", one)
	}
}
