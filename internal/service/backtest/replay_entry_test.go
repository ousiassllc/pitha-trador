package backtest_test

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest"
	"github.com/ousiassllc/pitha-trador/internal/service/featureengine"
)

// barsAtTimes is rampBars over explicit timestamps (so tests can model
// session gaps), with Feature recomputed bar-by-bar as
// VerifyNoLookahead expects.
func barsAtTimes(instrumentID int64, times []time.Time, startPrice, priceMultiplier float64) []domain.Snapshot {
	bars := make([]domain.Snapshot, 0, len(times))
	price := startPrice
	for _, ts := range times {
		feature := featureengine.Compute(featureengine.Input{
			Timestamp: ts,
			Current:   featureengine.Reading{Price: price, VWAP: price},
			History:   bars,
		})
		bars = append(bars, domain.Snapshot{
			InstrumentID: instrumentID, Timestamp: ts, Price: price, SpreadBps: ptr(10.0), Feature: feature,
		})
		price *= priceMultiplier
	}
	return bars
}

func shadowTrades(t *testing.T, instrumentID int64, bars []domain.Snapshot, decisions []domain.JevDecision) []backtest.Trade {
	t.Helper()
	cfg := backtest.RunConfig{
		InstrumentID: instrumentID,
		Symbol:       "7203",
		Snapshots:    bars,
		Decisions:    backtest.NewSliceDecisionSource(decisions),
		Thresholds:   testThresholds(),
		Exit:         backtest.ExitRule{StopLossPct: 5, TakeProfitPct: 1.0, MaxHolding: 10 * time.Minute},
	}
	period := backtest.Period{Start: bars[0].Timestamp, End: bars[len(bars)-1].Timestamp.Add(time.Minute)}
	trades, err := backtest.ShadowBacktestTrades(context.Background(), cfg, period)
	if err != nil {
		t.Fatalf("ShadowBacktestTrades: %v", err)
	}
	return trades
}

// TestReplay_DecisionConsumedAfterExit confirms FR-BT-4's "1判断→最大1
// エントリー": after a trade exits, the same decision is not re-used, so
// no re-entry happens until a new decision is recorded.
func TestReplay_DecisionConsumedAfterExit(t *testing.T) {
	const instrumentID = int64(1)
	base := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	bars := rampBars(instrumentID, base, 12, 2000, 1.005, 10) // +1% TP reached after 2 bars

	trades := shadowTrades(t, instrumentID, bars, []domain.JevDecision{longDecision(instrumentID, base)})
	if len(trades) != 1 {
		t.Fatalf("len(trades) = %d, want 1 (one decision must open at most one trade)", len(trades))
	}

	// A new decision after the exit opens exactly one more trade.
	trades = shadowTrades(t, instrumentID, bars, []domain.JevDecision{
		longDecision(instrumentID, base),
		longDecision(instrumentID, base.Add(6*time.Minute)),
	})
	if len(trades) != 2 {
		t.Fatalf("len(trades) = %d, want 2 (one per decision)", len(trades))
	}
}

// TestReplay_DecisionDuringOpenPositionIsDropped confirms a decision
// recorded while a position is open is not carried over to after the exit.
func TestReplay_DecisionDuringOpenPositionIsDropped(t *testing.T) {
	const instrumentID = int64(1)
	base := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	bars := rampBars(instrumentID, base, 12, 2000, 1.005, 10) // trade holds bars 0-2

	trades := shadowTrades(t, instrumentID, bars, []domain.JevDecision{
		longDecision(instrumentID, base),
		longDecision(instrumentID, base.Add(1*time.Minute)),
	})
	if len(trades) != 1 {
		t.Fatalf("len(trades) = %d, want 1 (decision at +1m arrived while the position was open)", len(trades))
	}
}

// TestReplay_EntryWithoutHoldingBarIsNotATrade covers #479: an entry on the
// last bar, or on a bar whose next bar is already past MaxHolding (session
// gap), must not produce an entry==exit zero-return trade.
func TestReplay_EntryWithoutHoldingBarIsNotATrade(t *testing.T) {
	const instrumentID = int64(1)
	d := func(h, m int) time.Time { return time.Date(2026, 1, 1, h, m, 0, 0, time.UTC) }

	t.Run("last bar", func(t *testing.T) {
		bars := barsAtTimes(instrumentID, []time.Time{d(9, 0), d(9, 1), d(9, 2)}, 2000, 1.001)
		trades := shadowTrades(t, instrumentID, bars, []domain.JevDecision{longDecision(instrumentID, d(9, 2))})
		if len(trades) != 0 {
			t.Fatalf("len(trades) = %d, want 0: %+v", len(trades), trades)
		}
	})

	t.Run("next bar past MaxHolding", func(t *testing.T) {
		bars := barsAtTimes(instrumentID, []time.Time{d(11, 28), d(11, 29), d(12, 30), d(12, 31)}, 2000, 1.001)
		trades := shadowTrades(t, instrumentID, bars, []domain.JevDecision{longDecision(instrumentID, d(11, 29))})
		if len(trades) != 0 {
			t.Fatalf("len(trades) = %d, want 0: %+v", len(trades), trades)
		}
	})
}
