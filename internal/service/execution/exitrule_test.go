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

func TestEngine_EvaluateExit(t *testing.T) {
	openedAt := time.Date(2026, 9, 27, 9, 30, 0, 0, time.UTC)
	marketClose := time.Date(2026, 9, 27, 15, 0, 0, 0, time.UTC)
	short, long := domain.JevDirectionShort, domain.JevDirectionLong
	lowContinuation, vwap := 0.40, 2101.0 // LONG position, price below VWAP => 逆クロス
	base := execution.Config{StopLossPct: 0.6, TakeProfitPct: 1.2}
	cfg := func(mut func(*execution.Config)) execution.Config { c := base; mut(&c); return c }

	tests := []struct {
		name   string
		cfg    execution.Config
		mkt    execution.MarketContext
		reason string // "" = no exit
	}{
		{"stop loss (-0.7% > 0.6%)", base, execution.MarketContext{Price: 2100.0 * (1 - 0.007)}, domain.ExitReasonStopLoss},
		{"take profit (+1.5% > 1.2%)", base, execution.MarketContext{Price: 2100.0 * 1.015}, domain.ExitReasonTakeProfit},
		{"nothing triggered", cfg(func(c *execution.Config) { c.MaxHoldingMinutes = 20 }),
			execution.MarketContext{Price: 2101.0, Now: openedAt.Add(time.Minute)}, ""},
		{"max holding", cfg(func(c *execution.Config) { c.MaxHoldingMinutes = 20 }),
			execution.MarketContext{Price: 2101.0, Now: openedAt.Add(21 * time.Minute)}, domain.ExitReasonMaxHolding},
		{"force flat before close", cfg(func(c *execution.Config) { c.ForceFlatBeforeMarketCloseMinutes = 10 }),
			execution.MarketContext{Price: 2101.0, Now: marketClose.Add(-9 * time.Minute), MarketCloseAt: &marketClose}, domain.ExitReasonForceFlatBeforeClose},
		{"jev direction reversal", base,
			execution.MarketContext{Price: 2101.0, Decision: &domain.JevDecision{Direction: &short}}, domain.ExitReasonJevDirectionReversed},
		{"continuation probability drop", cfg(func(c *execution.Config) { c.MinContinuationProbability = 0.60 }),
			execution.MarketContext{Price: 2101.0, Decision: &domain.JevDecision{Direction: &long, ContinuationProbability: &lowContinuation}}, domain.ExitReasonContinuationProbDrop},
		// FR-EXIT-3: a nil Decision (Jev API unresponsive) must not stop Stop Loss.
		{"stop loss still works without a Jev decision", base,
			execution.MarketContext{Price: 2100.0 * (1 - 0.01), Decision: nil}, domain.ExitReasonStopLoss},
		{"vwap reverse cross", base, execution.MarketContext{Price: 2100.5, VWAP: &vwap}, domain.ExitReasonVWAPCross},
		{"trailing stop without meaningful retrace", cfg(func(c *execution.Config) { c.StopLossPct, c.TakeProfitPct, c.TrailingStopPct = 5, 5, 0.5 }),
			execution.MarketContext{Price: 2100.0 * (1 + 0.002)}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			te := newTestEngine(t, tc.cfg)
			position := openLongPosition(t, te, 2100.0, openedAt)
			reason, triggered, err := te.engine.EvaluateExit(context.Background(), position, tc.mkt)
			if err != nil {
				t.Fatalf("EvaluateExit: %v", err)
			}
			if reason != tc.reason || triggered != (tc.reason != "") {
				t.Fatalf("EvaluateExit() = (%q, %v), want (%q, %v)", reason, triggered, tc.reason, tc.reason != "")
			}
		})
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
