package replayflow_test

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/domain/marketregime"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest"
)

// issue #755: the backtest metrics are split by the regime of the decision
// that opened each trade.
func TestAggregateByRegime_SplitsTradesAndOrdersRegimes(t *testing.T) {
	win := func(regime string, pct float64) backtest.Trade {
		return backtest.Trade{Regime: regime, NetReturnPct: pct}
	}
	got := backtest.AggregateByRegime([]backtest.Trade{
		win(marketregime.MeanRevert, 1.0),
		win(marketregime.Trend, 0.5),
		win(marketregime.Trend, -0.1),
		win("", 2.0),
	})

	if len(got) != 3 {
		t.Fatalf("AggregateByRegime = %+v, want trend, mean_revert and the unassessed group (unknown has no trades)", got)
	}
	want := []string{marketregime.Trend, marketregime.MeanRevert, ""}
	for i, r := range want {
		if got[i].Regime != r {
			t.Fatalf("regime[%d] = %q, want %q", i, got[i].Regime, r)
		}
	}
	if got[0].Metrics.TradeCount != 2 || got[0].Metrics.WinRate != 0.5 {
		t.Errorf("trend = %+v, want 2 trades, win rate 0.5", got[0].Metrics)
	}
	if got[1].Metrics.TradeCount != 1 || got[1].Metrics.WinRate != 1 {
		t.Errorf("mean_revert = %+v, want 1 winning trade", got[1].Metrics)
	}
}

func TestAggregateByRegime_NoTrades(t *testing.T) {
	if got := backtest.AggregateByRegime(nil); len(got) != 0 {
		t.Fatalf("AggregateByRegime(nil) = %+v, want none", got)
	}
}

// issue #755: a replayed trade carries the regime and arm of its opening
// decision, and Run reports the Forward trades by regime.
func TestRun_ReportsTradesByRegime(t *testing.T) {
	const instrumentID = int64(1)
	base := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	bars := rampBars(instrumentID, base, 15, 2000, 1.005, 10)

	decisions := make([]domain.JevDecision, 0, len(bars))
	for _, b := range bars {
		d := longDecision(instrumentID, b.Timestamp)
		d.MarketRegime = marketregime.Assessment{Regime: marketregime.Trend, ABArm: marketregime.ArmRegime}
		decisions = append(decisions, d)
	}
	cfg := backtest.RunConfig{
		InstrumentID: instrumentID, Symbol: "7203", Snapshots: bars,
		Decisions:  backtest.NewSliceDecisionSource(decisions),
		Models:     takingModels(),
		Thresholds: testThresholds(),
		Exit:       backtest.ExitRule{StopLossPct: 5, TakeProfitPct: 1.0, MaxHolding: 10 * time.Minute},
	}
	wf := backtest.WalkForwardConfig{
		Start: base, End: base.Add(10 * time.Minute),
		TrainingPeriod: 5 * time.Minute, ValidationPeriod: 3 * time.Minute, ForwardPeriod: 2 * time.Minute,
	}

	result, err := backtest.Run(context.Background(), cfg, wf)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(result.ByRegime) != 1 || result.ByRegime[0].Regime != marketregime.Trend {
		t.Fatalf("ByRegime = %+v, want only the trend group", result.ByRegime)
	}
	if result.ByRegime[0].Metrics.TradeCount != result.Combined.TradeCount || result.Combined.TradeCount == 0 {
		t.Errorf("trend trades = %d, Combined = %d, want the same non-zero count", result.ByRegime[0].Metrics.TradeCount, result.Combined.TradeCount)
	}
}
