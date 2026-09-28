package bootstrap

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest"
	"github.com/ousiassllc/pitha-trador/internal/service/featureengine"
)

// recordLiveBars persists n 1-minute bars for inst exactly the way the
// live market-data job does (Feature computed from the
// featureengine.HistoryLookbackBars most recent persisted bars), with a
// steadily rising price and a Jev Trader LONG decision recorded at every
// bar whose response_json clears config/strategy.yaml's LONG thresholds.
func recordLiveBars(t *testing.T, svc *Services, inst domain.Instrument, base time.Time, n int) {
	t.Helper()
	ctx := context.Background()
	spread := 10.0
	response, _ := json.Marshal(map[string]any{
		"entry_quality": domain.JevEntryQualityStrong, "continuation_probability": 0.8,
		"toxic_flow": 0.1, "liquidity_stressed": 0.1,
	})
	direction, confidence := domain.JevDirectionLong, 0.8
	var volume int64
	for i := 0; i < n; i++ {
		ts := base.Add(time.Duration(i) * time.Minute)
		price := 2000 * (1 + 0.002*float64(i))
		volume += int64(1000 + (i*i%13)*400)
		history, err := svc.Snapshots.ListByInstrument(ctx, inst.ID, featureengine.HistoryLookbackBars)
		if err != nil {
			t.Fatalf("ListByInstrument: %v", err)
		}
		feature := featureengine.Compute(featureengine.Input{
			Timestamp: ts, Current: featureengine.Reading{Price: price, VWAP: price, Volume: volume}, History: history,
		})
		if _, err := svc.Snapshots.Insert(ctx, domain.Snapshot{
			InstrumentID: inst.ID, Symbol: inst.Symbol, Timestamp: ts, Price: price,
			SpreadBps: &spread, Volume: volume, Feature: feature,
		}); err != nil {
			t.Fatalf("insert snapshot %d: %v", i, err)
		}
		if _, err := svc.Decisions.Insert(ctx, domain.JevDecision{
			InstrumentID: inst.ID, Symbol: inst.Symbol, Timestamp: ts, DecisionType: domain.JevDecisionTypeTrader,
			StateHash: "hash", StateJSON: "{}", QuestionVersion: "v1", ResponseJSON: string(response),
			Direction: &direction, Confidence: &confidence, ModelID: "test-model",
		}); err != nil {
			t.Fatalf("insert trader decision %d: %v", i, err)
		}
	}
}

func TestBacktestSource_RunWalkForwardReplaysRecordedHistory(t *testing.T) {
	svc := newTestServices(t, nil)
	inst := mustCreateInstrument(t, svc, "7203")
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	recordLiveBars(t, svc, inst, base, 90)

	// Start the backtest mid-history so the first bars in range need the
	// warmup bars before them to pass the look-ahead check.
	start := base.Add(30 * time.Minute)
	result, err := svc.Backtest.RunWalkForward(context.Background(), backtest.WalkForwardConfig{
		Start: start, End: start.Add(60 * time.Minute),
		TrainingPeriod: 20 * time.Minute, ValidationPeriod: 10 * time.Minute, ForwardPeriod: 10 * time.Minute,
	})
	if err != nil {
		t.Fatalf("RunWalkForward: %v", err)
	}
	if len(result.Splits) != 3 {
		t.Fatalf("len(Splits) = %d, want 3", len(result.Splits))
	}
	if result.Combined.TradeCount == 0 || result.Combined.Expectancy <= 0 {
		t.Errorf("Combined = %+v, want profitable Forward trades from the recorded LONG decisions on a rising price", result.Combined)
	}
	for i, sp := range result.Splits {
		if sp.Training.TradeCount != 0 {
			t.Errorf("Splits[%d].Training.TradeCount = %d, want 0", i, sp.Training.TradeCount)
		}
	}
}
