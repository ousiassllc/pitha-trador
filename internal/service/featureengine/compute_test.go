package featureengine_test

import (
	"math"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/featureengine"
)

func minutesAgo(base time.Time, m int) time.Time {
	return base.Add(-time.Duration(m) * time.Minute)
}

func ptr(v float64) *float64 { return &v }

func approxEqual(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}

func TestCompute_VWAPAlwaysPopulated(t *testing.T) {
	now := time.Date(2026, 9, 27, 9, 30, 0, 0, time.UTC)
	feature := featureengine.Compute(featureengine.Input{
		Timestamp: now,
		Current:   featureengine.Reading{Price: 2100, VWAP: 2000},
	})

	if feature.VWAP != 2000 {
		t.Errorf("VWAP = %v, want 2000", feature.VWAP)
	}
	wantBps := (2100.0 - 2000.0) / 2000.0 * 10000
	if feature.PriceVsVWAPBps != wantBps {
		t.Errorf("PriceVsVWAPBps = %v, want %v", feature.PriceVsVWAPBps, wantBps)
	}
}

func TestCompute_ReturnsNilWithoutHistory(t *testing.T) {
	now := time.Date(2026, 9, 27, 9, 30, 0, 0, time.UTC)
	feature := featureengine.Compute(featureengine.Input{
		Timestamp: now,
		Current:   featureengine.Reading{Price: 2100, VWAP: 2100},
	})

	if feature.Return1m != nil || feature.Return5m != nil || feature.Return15m != nil {
		t.Errorf("returns = (%v, %v, %v), want all nil with no history", feature.Return1m, feature.Return5m, feature.Return15m)
	}
	if feature.VolumeRatio5m != nil {
		t.Errorf("VolumeRatio5m = %v, want nil with no history", feature.VolumeRatio5m)
	}
	if feature.RealizedVol5m != nil {
		t.Errorf("RealizedVol5m = %v, want nil with no history", feature.RealizedVol5m)
	}
}

func TestCompute_Return1mUsesBarOneMinuteBack(t *testing.T) {
	now := time.Date(2026, 9, 27, 9, 30, 0, 0, time.UTC)
	history := []domain.Snapshot{
		{Timestamp: minutesAgo(now, 1), Price: 2000},
		{Timestamp: minutesAgo(now, 5), Price: 1900},
	}

	feature := featureengine.Compute(featureengine.Input{
		Timestamp: now,
		Current:   featureengine.Reading{Price: 2100, VWAP: 2100},
		History:   history,
	})

	if feature.Return1m == nil {
		t.Fatal("Return1m = nil, want a value")
	}
	want := 2100.0/2000.0 - 1
	if !approxEqual(*feature.Return1m, want) {
		t.Errorf("Return1m = %v, want %v", *feature.Return1m, want)
	}

	if feature.Return5m == nil {
		t.Fatal("Return5m = nil, want a value")
	}
	want5 := 2100.0/1900.0 - 1
	if !approxEqual(*feature.Return5m, want5) {
		t.Errorf("Return5m = %v, want %v", *feature.Return5m, want5)
	}

}

func TestCompute_IgnoresBarsAtOrAfterTimestamp(t *testing.T) {
	now := time.Date(2026, 9, 27, 9, 30, 0, 0, time.UTC)
	history := []domain.Snapshot{
		// A bar timestamped after "now" must never influence the result
		// (FR-FE-1 look-ahead防止), even though its price would otherwise
		// produce a very different Return1m.
		{Timestamp: now.Add(30 * time.Second), Price: 9999},
		{Timestamp: minutesAgo(now, 1), Price: 2000},
	}

	feature := featureengine.Compute(featureengine.Input{
		Timestamp: now,
		Current:   featureengine.Reading{Price: 2100, VWAP: 2100},
		History:   history,
	})

	if feature.Return1m == nil {
		t.Fatal("Return1m = nil, want a value")
	}
	want := 2100.0/2000.0 - 1
	if !approxEqual(*feature.Return1m, want) {
		t.Errorf("Return1m = %v, want %v (future bar leaked into the result)", *feature.Return1m, want)
	}
}

func TestCompute_OrderbookImbalanceNilWithoutBoardData(t *testing.T) {
	now := time.Date(2026, 9, 27, 9, 30, 0, 0, time.UTC)
	feature := featureengine.Compute(featureengine.Input{
		Timestamp: now,
		Current:   featureengine.Reading{Price: 2100, VWAP: 2100},
	})
	if feature.OrderbookImbalance != nil {
		t.Errorf("OrderbookImbalance = %v, want nil (FR-FE-2, no board data)", *feature.OrderbookImbalance)
	}
}

func TestCompute_OrderbookImbalanceComputedWhenAvailable(t *testing.T) {
	now := time.Date(2026, 9, 27, 9, 30, 0, 0, time.UTC)
	feature := featureengine.Compute(featureengine.Input{
		Timestamp: now,
		Current: featureengine.Reading{
			Price: 2100, VWAP: 2100,
			BidQty: ptr(300), AskQty: ptr(100),
		},
	})
	if feature.OrderbookImbalance == nil {
		t.Fatal("OrderbookImbalance = nil, want a value")
	}
	want := (300.0 - 100.0) / (300.0 + 100.0)
	if *feature.OrderbookImbalance != want {
		t.Errorf("OrderbookImbalance = %v, want %v", *feature.OrderbookImbalance, want)
	}
}

func TestCompute_MarketAndSectorReturnPassThrough(t *testing.T) {
	now := time.Date(2026, 9, 27, 9, 30, 0, 0, time.UTC)
	marketReturn := 0.0123
	sectorReturn := -0.0045
	feature := featureengine.Compute(featureengine.Input{
		Timestamp:      now,
		Current:        featureengine.Reading{Price: 2100, VWAP: 2100},
		MarketReturn5m: &marketReturn,
		SectorReturn5m: &sectorReturn,
	})
	if feature.MarketReturn5m == nil || *feature.MarketReturn5m != marketReturn {
		t.Errorf("MarketReturn5m = %v, want %v", feature.MarketReturn5m, marketReturn)
	}
	if feature.SectorReturn5m == nil || *feature.SectorReturn5m != sectorReturn {
		t.Errorf("SectorReturn5m = %v, want %v", feature.SectorReturn5m, sectorReturn)
	}
}

func TestCompute_VolumeRatio5m(t *testing.T) {
	now := time.Date(2026, 9, 27, 9, 30, 0, 0, time.UTC)
	// Cumulative session volume climbing by 100/min for 15 minutes, then
	// the current bar jumps by 1000 over the trailing 5 minutes (10x the
	// established 500/5min baseline).
	var history []domain.Snapshot
	cum := int64(0)
	for m := 15; m >= 1; m-- {
		cum += 100
		history = append(history, domain.Snapshot{Timestamp: minutesAgo(now, m), Price: 2000, Volume: cum})
	}

	feature := featureengine.Compute(featureengine.Input{
		Timestamp: now,
		Current:   featureengine.Reading{Price: 2000, VWAP: 2000, Volume: cum + 1000},
		History:   history,
	})

	if feature.VolumeRatio5m == nil {
		t.Fatal("VolumeRatio5m = nil, want a value")
	}
	if *feature.VolumeRatio5m <= 1.5 {
		t.Errorf("VolumeRatio5m = %v, want > 1.5 (volume spiked above baseline)", *feature.VolumeRatio5m)
	}
}

func TestCompute_RealizedVol5mZeroForConstantPrice(t *testing.T) {
	now := time.Date(2026, 9, 27, 9, 30, 0, 0, time.UTC)
	var history []domain.Snapshot
	for m := 5; m >= 1; m-- {
		history = append(history, domain.Snapshot{Timestamp: minutesAgo(now, m), Price: 2000})
	}

	feature := featureengine.Compute(featureengine.Input{
		Timestamp: now,
		Current:   featureengine.Reading{Price: 2000, VWAP: 2000},
		History:   history,
	})

	if feature.RealizedVol5m == nil {
		t.Fatal("RealizedVol5m = nil, want a value")
	}
	if *feature.RealizedVol5m != 0 {
		t.Errorf("RealizedVol5m = %v, want 0 for a constant price series", *feature.RealizedVol5m)
	}
}

func TestReturnOverWindow_NilWithoutSufficientHistory(t *testing.T) {
	now := time.Date(2026, 9, 27, 9, 30, 0, 0, time.UTC)
	got := featureengine.ReturnOverWindow(now, 2100, nil, 5*time.Minute)
	if got != nil {
		t.Errorf("ReturnOverWindow = %v, want nil", *got)
	}
}

func TestReturnOverWindow_UsedForIndexReturns(t *testing.T) {
	now := time.Date(2026, 9, 27, 9, 30, 0, 0, time.UTC)
	history := []domain.Snapshot{{Timestamp: minutesAgo(now, 5), Price: 2500}}
	got := featureengine.ReturnOverWindow(now, 2550, history, 5*time.Minute)
	if got == nil {
		t.Fatal("ReturnOverWindow = nil, want a value")
	}
	want := 2550.0/2500.0 - 1
	if !approxEqual(*got, want) {
		t.Errorf("ReturnOverWindow = %v, want %v", *got, want)
	}
}
