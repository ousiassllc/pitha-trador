package replayflow_test

import (
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest"
	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
)

// liveExit is FR-EXIT-2's initial values, as backtestsource passes them.
func liveExit() backtest.ExitRule {
	return backtest.ExitRule{
		StopLossPct: 1, TakeProfitPct: 2, TrailingStopPct: 0.5, MaxHolding: 20 * time.Minute,
		MinContinuationProbability: 0.6, ForceFlatBeforeClose: 10 * time.Minute, CooldownAfterLoss: 5 * time.Minute,
	}
}

// exitOf replays one LONG decision at minute 1 (the first bar with a 1-minute
// return) over prices/vwaps, plus extra decisions, and returns the one trade.
func exitOf(t *testing.T, prices, vwaps []float64, exit backtest.ExitRule, extra ...domain.JevDecision) backtest.Trade {
	t.Helper()
	bars := minuteBars(1, base, prices, vwaps)
	decisions := append([]domain.JevDecision{longDecision(1, at(1))}, extra...)
	trades := replayTrades(t, replayConfig(1, "7203", bars, decisions, exit))
	if len(trades) != 1 {
		t.Fatalf("len(trades) = %d, want 1: %+v", len(trades), trades)
	}
	return trades[0]
}

func wantExit(t *testing.T, trade backtest.Trade, reason string, exitMinute int) {
	t.Helper()
	if trade.ExitReason != reason || !trade.ExitTimestamp.Equal(at(exitMinute)) {
		t.Fatalf("exit = %q at minute %v, want %q at minute %d", trade.ExitReason, trade.ExitTimestamp.Sub(base).Minutes(), reason, exitMinute)
	}
}

func TestReplayExit_StopLoss(t *testing.T) {
	trade := exitOf(t, []float64{1000, 1000, 985, 985}, nil, liveExit())
	wantExit(t, trade, domain.ExitReasonStopLoss, 2)
	if trade.NetReturnPct >= 0 {
		t.Errorf("NetReturnPct = %v, want a loss", trade.NetReturnPct)
	}
}

func TestReplayExit_TakeProfit(t *testing.T) {
	wantExit(t, exitOf(t, []float64{1000, 1000, 1030, 1030}, nil, liveExit()), domain.ExitReasonTakeProfit, 2)
}

func TestReplayExit_TrailingStop(t *testing.T) {
	// +1.1% at the peak (below take profit), then 0.59% off it.
	wantExit(t, exitOf(t, []float64{1000, 1000, 1012, 1006, 1006}, nil, liveExit()), domain.ExitReasonTrailingStop, 3)
}

func TestReplayExit_JevDirectionReversal(t *testing.T) {
	reversed := longDecision(1, at(2))
	reversed.Direction = ptr(domain.JevDirectionShort)
	wantExit(t, exitOf(t, []float64{1000, 1000, 1001, 1001}, nil, liveExit(), reversed), domain.ExitReasonJevDirectionReversed, 2)
}

func TestReplayExit_ContinuationProbabilityDrop(t *testing.T) {
	dropped := longDecision(1, at(2))
	dropped.ContinuationProbability = ptr(0.3) // below min(0.6, the entry threshold 0.60)
	wantExit(t, exitOf(t, []float64{1000, 1000, 1001, 1001}, nil, liveExit(), dropped), domain.ExitReasonContinuationProbDrop, 2)

	// The limit is the lower of the exit setting and the entry threshold in force.
	cfgExit := liveExit()
	cfgExit.MinContinuationProbability = 0.2 // 0.3 is above it: no exit
	if got := exitOf(t, []float64{1000, 1000, 1001, 1001}, nil, cfgExit, dropped); got.ExitReason == domain.ExitReasonContinuationProbDrop {
		t.Fatalf("exited on continuation 0.3 with a 0.2 limit")
	}
}

func TestReplayExit_VWAPReverseCross(t *testing.T) {
	// Above VWAP at minute 2 (favourable, the baseline), then below at minute 3
	// by less than the trailing stop and the stop loss allow.
	prices := []float64{1000, 1000, 1004, 999, 999}
	vwaps := []float64{1000, 1000, 1000, 1000, 1000}
	wantExit(t, exitOf(t, prices, vwaps, liveExit()), domain.ExitReasonVWAPCross, 3)
}

func TestReplayExit_VWAPStayingOnTheAdverseSideIsNotACross(t *testing.T) {
	// Entered below VWAP and never above it: no cross, so the position holds to max holding.
	prices := []float64{1000, 1000, 999, 999, 999}
	vwaps := []float64{1005, 1005, 1005, 1005, 1005}
	exit := liveExit()
	exit.MaxHolding = 3 * time.Minute
	wantExit(t, exitOf(t, prices, vwaps, exit), domain.ExitReasonMaxHolding, 4)
}

func TestReplayExit_MaxHolding(t *testing.T) {
	exit := liveExit()
	exit.MaxHolding = 3 * time.Minute
	wantExit(t, exitOf(t, []float64{1000, 1000, 1000, 1000, 1000, 1000}, nil, exit), domain.ExitReasonMaxHolding, 4)
}

func jst(h, m int) time.Time { return time.Date(2026, 9, 29, h, m, 0, 0, marketcalendar.JST) } // 火曜: 立会日

func TestReplayExit_ForceFlatBeforeClose(t *testing.T) {
	// The close is 15:30; the force-flat time 15:20. Decide at 15:16, exit at 15:20.
	prices := []float64{1000, 1000, 1000, 1000, 1000, 1000, 1000, 1000}
	bars := minuteBars(1, jst(15, 15), prices, nil)
	cfg := replayConfig(1, "7203", bars, []domain.JevDecision{longDecision(1, jst(15, 16))}, liveExit())
	cfg.Sessions = marketcalendar.TSE

	trades := replayTrades(t, cfg)
	if len(trades) != 1 || trades[0].ExitReason != domain.ExitReasonForceFlatBeforeClose || !trades[0].ExitTimestamp.Equal(jst(15, 20)) {
		t.Fatalf("trades = %+v, want one force_flat_before_close exit at 15:20", trades)
	}
}

func TestReplayEntry_NoNewEntryFromTheForceFlatTimeOn(t *testing.T) {
	prices := []float64{1000, 1000, 1000, 1000, 1000, 1000, 1000, 1000}
	bars := minuteBars(1, jst(15, 15), prices, nil)
	cfg := replayConfig(1, "7203", bars, []domain.JevDecision{longDecision(1, jst(15, 21))}, liveExit())
	cfg.Sessions = marketcalendar.TSE
	if trades := replayTrades(t, cfg); len(trades) != 0 {
		t.Fatalf("trades = %+v, want none: an entry after 15:20 would be flattened at once", trades)
	}
}

func TestReplayExit_DataEndedHoldsToTheLastBar(t *testing.T) {
	exit := liveExit()
	exit.MaxHolding = time.Hour
	got := exitOf(t, []float64{1000, 1000, 1001, 1001}, nil, exit)
	wantExit(t, got, backtest.ExitReasonDataEnded, 3)
}

func TestReplayEntry_SymbolCooldownAfterALossingClose(t *testing.T) {
	// A stop-out at minute 2; the next decision at minute 4 is inside the 5-minute
	// cooldown, the one at minute 8 is not.
	prices := []float64{1000, 1000, 985, 1000, 1000, 1000, 1000, 1000, 1000, 1000, 1000}
	bars := minuteBars(1, base, prices, nil)
	run := func(second int) []backtest.Trade {
		decisions := []domain.JevDecision{longDecision(1, at(1)), longDecision(1, at(second))}
		return replayTrades(t, replayConfig(1, "7203", bars, decisions, liveExit()))
	}
	if got := run(4); len(got) != 1 {
		t.Fatalf("decision inside the cooldown: %d trades, want 1: %+v", len(got), got)
	}
	if got := run(8); len(got) != 2 {
		t.Fatalf("decision after the cooldown: %d trades, want 2: %+v", len(got), got)
	}
}
