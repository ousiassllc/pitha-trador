package execution_test

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/execution"
)

func openLongPosition(t *testing.T, te testEngine, entryPrice float64, openedAt time.Time) domain.Position {
	t.Helper()
	result, err := te.engine.Enter(context.Background(), execution.EntryRequest{
		Signal: longSignal(te.instrument.ID), Quantity: 100,
		OrderType: domain.OrderTypeMarket, Price: entryPrice, Now: openedAt,
	})
	if err != nil {
		t.Fatalf("Enter (fixture): %v", err)
	}
	return *result.Position
}

func TestEngine_EvaluateExit_StopLossTriggers(t *testing.T) {
	te := newTestEngine(t, execution.Config{StopLossPct: 0.6, TakeProfitPct: 1.2})
	position := openLongPosition(t, te, 2100.0, time.Now().UTC())

	reason, triggered, err := te.engine.EvaluateExit(context.Background(), position, execution.MarketContext{
		Price: 2100.0 * (1 - 0.007), // -0.7% > 0.6% stop loss threshold
	})
	if err != nil {
		t.Fatalf("EvaluateExit: %v", err)
	}
	if !triggered || reason != domain.ExitReasonStopLoss {
		t.Fatalf("EvaluateExit() = (%q, %v), want (%q, true)", reason, triggered, domain.ExitReasonStopLoss)
	}
}

func TestEngine_EvaluateExit_TakeProfitTriggers(t *testing.T) {
	te := newTestEngine(t, execution.Config{StopLossPct: 0.6, TakeProfitPct: 1.2})
	position := openLongPosition(t, te, 2100.0, time.Now().UTC())

	reason, triggered, err := te.engine.EvaluateExit(context.Background(), position, execution.MarketContext{
		Price: 2100.0 * 1.015, // +1.5% > 1.2% take profit threshold
	})
	if err != nil {
		t.Fatalf("EvaluateExit: %v", err)
	}
	if !triggered || reason != domain.ExitReasonTakeProfit {
		t.Fatalf("EvaluateExit() = (%q, %v), want (%q, true)", reason, triggered, domain.ExitReasonTakeProfit)
	}
}

func TestEngine_EvaluateExit_NoConditionTriggeredYet(t *testing.T) {
	te := newTestEngine(t, execution.Config{StopLossPct: 0.6, TakeProfitPct: 1.2, MaxHoldingMinutes: 20})
	openedAt := time.Now().UTC()
	position := openLongPosition(t, te, 2100.0, openedAt)

	_, triggered, err := te.engine.EvaluateExit(context.Background(), position, execution.MarketContext{
		Price: 2101.0, Now: openedAt.Add(1 * time.Minute),
	})
	if err != nil {
		t.Fatalf("EvaluateExit: %v", err)
	}
	if triggered {
		t.Fatalf("EvaluateExit() triggered = true, want false (price within every threshold)")
	}
}

func TestEngine_EvaluateExit_MaxHoldingTriggers(t *testing.T) {
	te := newTestEngine(t, execution.Config{StopLossPct: 0.6, TakeProfitPct: 1.2, MaxHoldingMinutes: 20})
	openedAt := time.Date(2026, 9, 27, 9, 30, 0, 0, time.UTC)
	position := openLongPosition(t, te, 2100.0, openedAt)

	reason, triggered, err := te.engine.EvaluateExit(context.Background(), position, execution.MarketContext{
		Price: 2101.0, Now: openedAt.Add(21 * time.Minute),
	})
	if err != nil {
		t.Fatalf("EvaluateExit: %v", err)
	}
	if !triggered || reason != domain.ExitReasonMaxHolding {
		t.Fatalf("EvaluateExit() = (%q, %v), want (%q, true)", reason, triggered, domain.ExitReasonMaxHolding)
	}
}

func TestEngine_EvaluateExit_ForceFlatBeforeMarketCloseTriggers(t *testing.T) {
	te := newTestEngine(t, execution.Config{
		StopLossPct: 0.6, TakeProfitPct: 1.2, MaxHoldingMinutes: 0, ForceFlatBeforeMarketCloseMinutes: 10,
	})
	openedAt := time.Date(2026, 9, 27, 9, 30, 0, 0, time.UTC)
	position := openLongPosition(t, te, 2100.0, openedAt)
	marketClose := time.Date(2026, 9, 27, 15, 0, 0, 0, time.UTC)

	reason, triggered, err := te.engine.EvaluateExit(context.Background(), position, execution.MarketContext{
		Price: 2101.0, Now: marketClose.Add(-9 * time.Minute), MarketCloseAt: &marketClose,
	})
	if err != nil {
		t.Fatalf("EvaluateExit: %v", err)
	}
	if !triggered || reason != domain.ExitReasonForceFlatBeforeClose {
		t.Fatalf("EvaluateExit() = (%q, %v), want (%q, true)", reason, triggered, domain.ExitReasonForceFlatBeforeClose)
	}
}

func TestEngine_EvaluateExit_JevDirectionReversalTriggers(t *testing.T) {
	te := newTestEngine(t, execution.Config{StopLossPct: 0.6, TakeProfitPct: 1.2})
	position := openLongPosition(t, te, 2100.0, time.Now().UTC())
	short := domain.JevDirectionShort

	reason, triggered, err := te.engine.EvaluateExit(context.Background(), position, execution.MarketContext{
		Price: 2101.0, Decision: &domain.JevDecision{Direction: &short},
	})
	if err != nil {
		t.Fatalf("EvaluateExit: %v", err)
	}
	if !triggered || reason != domain.ExitReasonJevDirectionReversed {
		t.Fatalf("EvaluateExit() = (%q, %v), want (%q, true)", reason, triggered, domain.ExitReasonJevDirectionReversed)
	}
}

func TestEngine_EvaluateExit_ContinuationProbabilityDropTriggers(t *testing.T) {
	te := newTestEngine(t, execution.Config{StopLossPct: 0.6, TakeProfitPct: 1.2, MinContinuationProbability: 0.60})
	position := openLongPosition(t, te, 2100.0, time.Now().UTC())
	long := domain.JevDirectionLong
	lowContinuation := 0.40

	reason, triggered, err := te.engine.EvaluateExit(context.Background(), position, execution.MarketContext{
		Price: 2101.0,
		Decision: &domain.JevDecision{
			Direction: &long, ContinuationProbability: &lowContinuation,
		},
	})
	if err != nil {
		t.Fatalf("EvaluateExit: %v", err)
	}
	if !triggered || reason != domain.ExitReasonContinuationProbDrop {
		t.Fatalf("EvaluateExit() = (%q, %v), want (%q, true)", reason, triggered, domain.ExitReasonContinuationProbDrop)
	}
}

func TestEngine_EvaluateExit_JevAPIUnavailable_OtherConditionsStillWork(t *testing.T) {
	// FR-EXIT-3: a nil Decision (Jev API unresponsive) must not stop
	// Stop Loss from still working.
	te := newTestEngine(t, execution.Config{StopLossPct: 0.6, TakeProfitPct: 1.2})
	position := openLongPosition(t, te, 2100.0, time.Now().UTC())

	reason, triggered, err := te.engine.EvaluateExit(context.Background(), position, execution.MarketContext{
		Price: 2100.0 * (1 - 0.01), Decision: nil,
	})
	if err != nil {
		t.Fatalf("EvaluateExit: %v", err)
	}
	if !triggered || reason != domain.ExitReasonStopLoss {
		t.Fatalf("EvaluateExit() = (%q, %v), want (%q, true) even with Decision == nil", reason, triggered, domain.ExitReasonStopLoss)
	}
}

func TestEngine_EvaluateExit_VWAPCrossTriggers(t *testing.T) {
	te := newTestEngine(t, execution.Config{StopLossPct: 0.6, TakeProfitPct: 1.2})
	position := openLongPosition(t, te, 2100.0, time.Now().UTC())
	vwap := 2101.0 // LONG position, price below VWAP => 逆クロス

	reason, triggered, err := te.engine.EvaluateExit(context.Background(), position, execution.MarketContext{
		Price: 2100.5, VWAP: &vwap,
	})
	if err != nil {
		t.Fatalf("EvaluateExit: %v", err)
	}
	if !triggered || reason != domain.ExitReasonVWAPCross {
		t.Fatalf("EvaluateExit() = (%q, %v), want (%q, true)", reason, triggered, domain.ExitReasonVWAPCross)
	}
}

func TestEngine_EvaluateExit_TrailingStopTriggersAfterRetraceFromPeak(t *testing.T) {
	te := newTestEngine(t, execution.Config{StopLossPct: 5, TakeProfitPct: 5, TrailingStopPct: 0.5})
	ctx := context.Background()
	openedAt := time.Date(2026, 9, 27, 9, 30, 0, 0, time.UTC)
	position := openLongPosition(t, te, 2100.0, openedAt)

	// Price rallies to a peak of 2130 shortly after entry, recorded via a
	// market_snapshots row (trailing stop has no positions column - see
	// exitrule.go's trailingStopTriggered doc comment).
	if _, err := te.snapshots.Insert(ctx, domain.Snapshot{
		InstrumentID: te.instrument.ID, Symbol: "7203",
		Timestamp: openedAt.Add(5 * time.Minute), Price: 2130.0,
		Volume: 1000, Turnover: 2130000, Feature: domain.Feature{VWAP: 2115.0},
	}); err != nil {
		t.Fatalf("seed peak snapshot: %v", err)
	}

	// Retraced 0.5%+ from the 2130 peak, but still within the +/-5%
	// stop-loss/take-profit band relative to the 2100 entry price - only
	// Trailing Stop should catch this.
	retraced := 2130.0 * (1 - 0.006)
	reason, triggered, err := te.engine.EvaluateExit(ctx, position, execution.MarketContext{
		Price: retraced, Now: openedAt.Add(6 * time.Minute),
	})
	if err != nil {
		t.Fatalf("EvaluateExit: %v", err)
	}
	if !triggered || reason != domain.ExitReasonTrailingStop {
		t.Fatalf("EvaluateExit() = (%q, %v), want (%q, true) after retracing from the recorded peak", reason, triggered, domain.ExitReasonTrailingStop)
	}
}

func TestEngine_EvaluateExit_TrailingStopDoesNotTriggerWithoutRetrace(t *testing.T) {
	te := newTestEngine(t, execution.Config{StopLossPct: 5, TakeProfitPct: 5, TrailingStopPct: 0.5})
	openedAt := time.Now().UTC()
	position := openLongPosition(t, te, 2100.0, openedAt)

	reason, triggered, err := te.engine.EvaluateExit(context.Background(), position, execution.MarketContext{
		Price: 2100.0 * (1 + 0.002),
	})
	if err != nil {
		t.Fatalf("EvaluateExit: %v", err)
	}
	if triggered {
		t.Fatalf("EvaluateExit() triggered = true (%q), want false with no meaningful retrace from entry", reason)
	}
}
