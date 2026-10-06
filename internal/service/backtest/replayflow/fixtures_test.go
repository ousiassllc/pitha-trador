package replayflow_test

import (
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/featureengine"
	"github.com/ousiassllc/pitha-trador/internal/service/policy"
)

func ptr[T any](v T) *T { return &v }

// testThresholds mirrors internal/service/policy's own test fixture
// (engine_test.go's testThresholds): the LONG boundary this package's
// longDecision fixture comfortably clears.
func testThresholds() policy.Thresholds {
	return policy.Thresholds{
		Policy: config.PolicyConfig{
			Long: config.PolicyDirectionThresholds{
				MinProbability: 0.68, MinEntryQuality: domain.JevEntryQualityStrong,
				MinContinuationProbability: 0.60, MaxToxicFlow: 0.35, MaxLiquidityStressed: 0.25,
			},
		},
		MaxSpreadBps: 50,
	}
}

// rampBars builds a look-ahead-safe Snapshot series (Feature recomputed
// bar-by-bar via featureengine.Compute, exactly as VerifyNoLookahead
// expects) with a steady compounding price increase and a fixed
// SpreadBps, for a deterministic LONG-side backtest replay.
func rampBars(instrumentID int64, base time.Time, n int, startPrice, priceMultiplier, spreadBps float64) []domain.Snapshot {
	bars := make([]domain.Snapshot, 0, n)
	price := startPrice
	for i := 0; i < n; i++ {
		ts := base.Add(time.Duration(i) * time.Minute)
		feature := featureengine.Compute(featureengine.Input{
			Timestamp: ts,
			Current:   featureengine.Reading{Price: price, VWAP: price},
			History:   bars,
		})
		bars = append(bars, domain.Snapshot{
			InstrumentID: instrumentID,
			Timestamp:    ts,
			Price:        price,
			SpreadBps:    ptr(spreadBps),
			Feature:      feature,
		})
		price *= priceMultiplier
	}
	return bars
}

// longDecision is a Jev Trader decision that comfortably clears
// testThresholds' LONG boundary (probability 0.80 > 0.68, etc.).
func longDecision(instrumentID int64, ts time.Time) domain.JevDecision {
	return domain.JevDecision{
		InstrumentID:            instrumentID,
		Timestamp:               ts,
		Direction:               ptr(domain.JevDirectionLong),
		EntryQuality:            ptr(domain.JevEntryQualityStrong),
		Confidence:              ptr(0.80),
		ContinuationProbability: ptr(0.70),
		ToxicFlow:               ptr(0.10),
		LiquidityStressed:       ptr(0.10),
	}
}
