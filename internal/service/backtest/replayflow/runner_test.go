package replayflow_test

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest"
)

// TestRun_EndToEnd_TrainingBlockedForwardTrades is the "最低限のバックテ
// スト実行〜指標算出が一気通貫で動作する" acceptance criterion: a full
// Walk Forward Run over a monotonic uptrend produces zero trades during
// Training/Calibration (FR-POLICY-3 ReasonNotCalibrated - Calibrated is
// false for that phase) and at least one profitable trade during
// Forward, aggregated into Combined (FR-BT-1/FR-BT-2).
func TestRun_EndToEnd_TrainingBlockedForwardTrades(t *testing.T) {
	const instrumentID = int64(1)
	base := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	bars := rampBars(instrumentID, base, 15, 2000, 1.005, 10)

	decisions := make([]domain.JevDecision, 0, len(bars))
	for _, b := range bars {
		decisions = append(decisions, longDecision(instrumentID, b.Timestamp))
	}

	cfg := backtest.RunConfig{
		InstrumentID: instrumentID,
		Symbol:       "7203",
		Snapshots:    bars,
		Decisions:    backtest.NewSliceDecisionSource(decisions),
		Models:       takingModels(),
		Thresholds:   testThresholds(),
		Exit:         backtest.ExitRule{StopLossPct: 5, TakeProfitPct: 1.0, MaxHolding: 10 * time.Minute},
	}
	wf := backtest.WalkForwardConfig{
		Start:            base,
		End:              base.Add(10 * time.Minute),
		TrainingPeriod:   5 * time.Minute,
		ValidationPeriod: 3 * time.Minute,
		ForwardPeriod:    2 * time.Minute,
	}

	result, err := backtest.Run(context.Background(), cfg, wf)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(result.Splits) != 1 {
		t.Fatalf("len(Splits) = %d, want 1", len(result.Splits))
	}

	sp := result.Splits[0]
	if sp.Training.TradeCount != 0 {
		t.Errorf("Training.TradeCount = %d, want 0 (Calibrated=false during Training/Calibration)", sp.Training.TradeCount)
	}
	if sp.Forward.TradeCount == 0 {
		t.Fatal("Forward.TradeCount = 0, want at least one trade")
	}
	if sp.Forward.Expectancy <= 0 {
		t.Errorf("Forward.Expectancy = %v, want > 0 for a monotonic uptrend LONG strategy", sp.Forward.Expectancy)
	}
	if result.Combined.TradeCount != sp.Forward.TradeCount {
		t.Errorf("Combined.TradeCount = %d, want %d (Combined aggregates Forward-period trades only, FR-BT-2 \"全期間一括最適化しない\")",
			result.Combined.TradeCount, sp.Forward.TradeCount)
	}
}

// TestRun_NoFoldsIsAnError confirms a WalkForwardConfig too short to
// produce even one fold is rejected rather than silently returning an
// empty, misleadingly-successful Result.
func TestRun_NoFoldsIsAnError(t *testing.T) {
	const instrumentID = int64(1)
	base := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	bars := rampBars(instrumentID, base, 3, 2000, 1.0, 10)

	cfg := backtest.RunConfig{
		InstrumentID: instrumentID,
		Symbol:       "7203",
		Snapshots:    bars,
		Decisions:    backtest.NewSliceDecisionSource(nil),
		Thresholds:   testThresholds(),
	}
	wf := backtest.WalkForwardConfig{
		Start: base, End: base.Add(time.Minute),
		TrainingPeriod: time.Hour, ValidationPeriod: time.Hour, ForwardPeriod: time.Hour,
	}

	if _, err := backtest.Run(context.Background(), cfg, wf); err == nil {
		t.Fatal("Run() error = nil, want an error for a range too short to fit one fold")
	}
}

// TestShadowBacktest_ThresholdOverrideChangesOutcome proves the
// "Policyしきい値を差し替え可能なインターフェース" a Self-Improvement
// Governor shadow backtest needs (overview.md §8 "GOV->>BT: ...提案後し
// きい値でのシャドーバックテスト実行"): the exact same fixture data
// produces a trade when RunConfig.Thresholds is one the decision clears,
// and produces none once Thresholds is tightened past it. Under the live
// rule the hard guards are the thresholds that bind (policy.*.min_probability
// is replaced by the model's verdict), so the continuation guard is tightened.
func TestShadowBacktest_ThresholdOverrideChangesOutcome(t *testing.T) {
	const instrumentID = int64(1)
	base := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	bars := rampBars(instrumentID, base, 10, 2000, 1.005, 10)

	decisions := make([]domain.JevDecision, 0, len(bars))
	for _, b := range bars {
		decisions = append(decisions, longDecision(instrumentID, b.Timestamp))
	}

	cfg := backtest.RunConfig{
		InstrumentID: instrumentID,
		Symbol:       "7203",
		Snapshots:    bars,
		Decisions:    backtest.NewSliceDecisionSource(decisions),
		Models:       takingModels(),
		Thresholds:   testThresholds(), // MinProbability 0.68 <= fixture's 0.80 confidence
		Exit:         backtest.ExitRule{StopLossPct: 5, TakeProfitPct: 1.0, MaxHolding: 10 * time.Minute},
	}
	period := backtest.Period{Start: base, End: base.Add(10 * time.Minute)}

	passing, err := backtest.ShadowBacktest(context.Background(), cfg, period)
	if err != nil {
		t.Fatalf("ShadowBacktest (clearing thresholds): %v", err)
	}
	if passing.TradeCount == 0 {
		t.Fatal("passing.TradeCount = 0, want at least one trade with thresholds the decision clears")
	}

	strict := cfg
	strictThresholds := testThresholds()
	strictThresholds.Policy.Long.MinContinuationProbability = 0.95 // above the fixture's 0.70
	strict.Thresholds = strictThresholds

	rejected, err := backtest.ShadowBacktest(context.Background(), strict, period)
	if err != nil {
		t.Fatalf("ShadowBacktest (stricter thresholds): %v", err)
	}
	if rejected.TradeCount != 0 {
		t.Errorf("rejected.TradeCount = %d, want 0: the Thresholds override must actually change the Policy Engine's evaluation", rejected.TradeCount)
	}
}

// TestSliceDecisionSource_ReturnsMostRecentAtOrBefore confirms
// SliceDecisionSource.Decision picks the latest decision at-or-before ts
// (not the nearest by absolute distance, and not any decision after ts).
func TestSliceDecisionSource_ReturnsMostRecentAtOrBefore(t *testing.T) {
	base := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	src := backtest.NewSliceDecisionSource([]domain.JevDecision{
		{InstrumentID: 1, Timestamp: base, Confidence: ptr(0.1)},
		{InstrumentID: 1, Timestamp: base.Add(2 * time.Minute), Confidence: ptr(0.2)},
		{InstrumentID: 1, Timestamp: base.Add(5 * time.Minute), Confidence: ptr(0.3)},
		{InstrumentID: 2, Timestamp: base.Add(2 * time.Minute), Confidence: ptr(0.9)},
	})

	got, ok, err := src.Decision(context.Background(), 1, base.Add(3*time.Minute))
	if err != nil {
		t.Fatalf("Decision: %v", err)
	}
	if !ok {
		t.Fatal("ok = false, want true")
	}
	if got.Confidence == nil || *got.Confidence != 0.2 {
		t.Errorf("Confidence = %v, want 0.2 (the latest decision at or before ts)", got.Confidence)
	}

	if _, ok, err := src.Decision(context.Background(), 1, base.Add(-time.Minute)); err != nil || ok {
		t.Errorf("Decision before any recorded decision: ok=%v err=%v, want ok=false err=nil", ok, err)
	}
}
