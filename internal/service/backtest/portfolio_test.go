package backtest_test

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest"
)

func longRunConfig(instrumentID int64, symbol string, bars []domain.Snapshot) backtest.RunConfig {
	decisions := make([]domain.JevDecision, 0, len(bars))
	for _, b := range bars {
		decisions = append(decisions, longDecision(instrumentID, b.Timestamp))
	}
	return backtest.RunConfig{
		InstrumentID: instrumentID,
		Symbol:       symbol,
		Snapshots:    bars,
		Decisions:    backtest.NewSliceDecisionSource(decisions),
		Thresholds:   testThresholds(),
		Exit:         backtest.ExitRule{StopLossPct: 5, TakeProfitPct: 1.0, MaxHolding: 10 * time.Minute},
	}
}

func TestRunPortfolio_CombinesEveryInstrumentsForwardTrades(t *testing.T) {
	base := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	up := longRunConfig(1, "7203", rampBars(1, base, 15, 2000, 1.005, 10))
	down := longRunConfig(2, "6758", rampBars(2, base, 15, 3000, 0.995, 10))
	wf := backtest.WalkForwardConfig{
		Start: base, End: base.Add(10 * time.Minute),
		TrainingPeriod: 5 * time.Minute, ValidationPeriod: 3 * time.Minute, ForwardPeriod: 2 * time.Minute,
	}
	ctx := context.Background()

	upOnly, err := backtest.Run(ctx, up, wf)
	if err != nil {
		t.Fatalf("Run(up): %v", err)
	}
	downOnly, err := backtest.Run(ctx, down, wf)
	if err != nil {
		t.Fatalf("Run(down): %v", err)
	}
	combined, err := backtest.RunPortfolio(ctx, []backtest.RunConfig{up, down}, wf)
	if err != nil {
		t.Fatalf("RunPortfolio: %v", err)
	}

	wantTrades := upOnly.Combined.TradeCount + downOnly.Combined.TradeCount
	if upOnly.Combined.TradeCount == 0 || downOnly.Combined.TradeCount == 0 {
		t.Fatalf("fixture produced no trades (up %d, down %d)", upOnly.Combined.TradeCount, downOnly.Combined.TradeCount)
	}
	if combined.Combined.TradeCount != wantTrades {
		t.Errorf("Combined.TradeCount = %d, want %d (both instruments' Forward trades)", combined.Combined.TradeCount, wantTrades)
	}
	if combined.Combined.WinRate <= 0 || combined.Combined.WinRate >= 1 {
		t.Errorf("Combined.WinRate = %v, want strictly between 0 and 1 for one winning and one losing instrument", combined.Combined.WinRate)
	}
}

func TestRunPortfolio_RejectsAnyInstrumentFailingLookaheadCheck(t *testing.T) {
	base := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	clean := longRunConfig(1, "7203", rampBars(1, base, 15, 2000, 1.005, 10))
	leakyBars := rampBars(2, base, 15, 3000, 1.005, 10)
	leak := 0.5
	leakyBars[10].Feature.Return1m = &leak
	leaky := longRunConfig(2, "6758", leakyBars)
	wf := backtest.WalkForwardConfig{
		Start: base, End: base.Add(10 * time.Minute),
		TrainingPeriod: 5 * time.Minute, ValidationPeriod: 3 * time.Minute, ForwardPeriod: 2 * time.Minute,
	}

	if _, err := backtest.RunPortfolio(context.Background(), []backtest.RunConfig{clean, leaky}, wf); err == nil {
		t.Fatal("RunPortfolio with one look-ahead-violating instrument = nil error, want FR-BT-3 rejection")
	}
}
