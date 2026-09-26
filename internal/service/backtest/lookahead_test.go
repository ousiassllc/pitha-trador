package backtest_test

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest"
	"github.com/ousiassllc/pitha-trador/internal/service/featureengine"
)

// buildCleanBars builds a genuinely look-ahead-safe Snapshot series: each
// bar's Feature is computed the same way the live Feature Engine
// produces it, using only the bars strictly before it as History
// (FR-FE-1).
func buildCleanBars(prices []float64, base time.Time) []domain.Snapshot {
	bars := make([]domain.Snapshot, 0, len(prices))
	for i, price := range prices {
		ts := base.Add(time.Duration(i) * time.Minute)
		feature := featureengine.Compute(featureengine.Input{
			Timestamp: ts,
			Current:   featureengine.Reading{Price: price, VWAP: price},
			History:   bars,
		})
		bars = append(bars, domain.Snapshot{Timestamp: ts, Price: price, Feature: feature})
	}
	return bars
}

// TestVerifyNoLookahead_CleanSeriesHasNoViolations confirms a genuinely
// look-ahead-safe series (the same shape internal/service/featureengine's
// own RunCycle produces bar-by-bar in the live pipeline) reports no
// FR-BT-3 violations.
func TestVerifyNoLookahead_CleanSeriesHasNoViolations(t *testing.T) {
	base := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	bars := buildCleanBars([]float64{2000, 2005, 2010, 2008, 2012, 2015, 2020, 2018, 2025, 2030}, base)

	if violations := backtest.VerifyNoLookahead(bars); len(violations) != 0 {
		t.Fatalf("VerifyNoLookahead = %+v, want no violations for a genuinely look-ahead-safe series", violations)
	}
}

// TestVerifyNoLookahead_DetectsLeakedFutureData proves FR-BT-3's "テスト
// 上" guarantee: a bar whose persisted Return5m was actually derived from
// a later bar's price - the exact shape of an externally-imported
// historical dataset, or a live-pipeline bug, that skipped Feature
// Engine's own FR-FE-1 boundary - is flagged rather than silently
// accepted.
func TestVerifyNoLookahead_DetectsLeakedFutureData(t *testing.T) {
	base := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	bars := buildCleanBars([]float64{2000, 2005, 2010, 2008, 2012, 2015, 2020, 2018, 2025, 2030}, base)

	// Bar 5 (t=+5min) genuinely has 5 minutes of prior history, so its
	// correct Return5m compares against bar 0's price (2000). Taint it
	// with a value computed against bar 9's price (2030) instead - data
	// four minutes in bar 5's own future.
	leaked := bars[9].Price/bars[5].Price - 1
	bars[5].Feature.Return5m = &leaked

	violations := backtest.VerifyNoLookahead(bars)
	found := false
	for _, v := range violations {
		if v.Index == 5 && v.Field == "Return5m" {
			found = true
		}
	}
	if !found {
		t.Fatalf("violations = %+v, want bar 5's tainted Return5m flagged (FR-BT-3)", violations)
	}
}

// TestRun_RejectsLookaheadTaintedData confirms Run enforces FR-BT-3 at
// the runtime-logic level (not merely as a standalone test helper): a
// tainted Snapshot series is rejected with an error rather than
// producing misleading backtest metrics.
func TestRun_RejectsLookaheadTaintedData(t *testing.T) {
	base := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	bars := buildCleanBars([]float64{2000, 2005, 2010, 2008, 2012, 2015, 2020, 2018, 2025, 2030}, base)
	leaked := bars[9].Price/bars[5].Price - 1
	bars[5].Feature.Return5m = &leaked

	cfg := backtest.RunConfig{
		InstrumentID: 1,
		Symbol:       "TAINT",
		Snapshots:    bars,
		Decisions:    backtest.NewSliceDecisionSource(nil),
	}
	wf := backtest.WalkForwardConfig{
		Start:            base,
		End:              base.Add(10 * time.Minute),
		TrainingPeriod:   3 * time.Minute,
		ValidationPeriod: 3 * time.Minute,
		ForwardPeriod:    2 * time.Minute,
	}

	if _, err := backtest.Run(context.Background(), cfg, wf); err == nil {
		t.Fatal("Run() error = nil, want an FR-BT-3 rejection for look-ahead-tainted input data")
	}
}
