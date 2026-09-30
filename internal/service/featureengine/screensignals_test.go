package featureengine_test

import (
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/featureengine"
)

// minuteBars builds one bar per minute, oldest first, ending one minute
// before base: prices[len-1] is the bar at base-1m.
func minuteBars(base time.Time, prices ...float64) []domain.Snapshot {
	bars := make([]domain.Snapshot, len(prices))
	for i, p := range prices {
		bars[i] = domain.Snapshot{Timestamp: minutesAgo(base, len(prices)-i), Price: p}
	}
	return bars
}

func TestComputeScreenSignals_BreakoutStrengthAboveRangeHigh(t *testing.T) {
	now := time.Date(2026, 9, 27, 9, 30, 0, 0, time.UTC)
	history := minuteBars(now, 1000, 1010, 1000, 1005, 1000)

	got := featureengine.ComputeScreenSignals(domain.Snapshot{Timestamp: now, Price: 1030}, history)

	if got.BreakoutStrength == nil {
		t.Fatal("BreakoutStrength = nil, want price/high - 1")
	}
	if want := 1030.0/1010.0 - 1; !approxEqual(*got.BreakoutStrength, want) {
		t.Errorf("BreakoutStrength = %v, want %v", *got.BreakoutStrength, want)
	}
}

func TestComputeScreenSignals_BreakoutStrengthBelowRangeLow(t *testing.T) {
	now := time.Date(2026, 9, 27, 9, 30, 0, 0, time.UTC)
	history := minuteBars(now, 1000, 1010, 1000, 1005, 1000)

	got := featureengine.ComputeScreenSignals(domain.Snapshot{Timestamp: now, Price: 980}, history)

	if got.BreakoutStrength == nil {
		t.Fatal("BreakoutStrength = nil, want 1 - price/low")
	}
	if want := 1 - 980.0/1000.0; !approxEqual(*got.BreakoutStrength, want) {
		t.Errorf("BreakoutStrength = %v, want %v", *got.BreakoutStrength, want)
	}
}

func TestComputeScreenSignals_BreakoutStrengthZeroInsideRange(t *testing.T) {
	now := time.Date(2026, 9, 27, 9, 30, 0, 0, time.UTC)
	history := minuteBars(now, 1000, 1010, 1000, 1005, 1000)

	got := featureengine.ComputeScreenSignals(domain.Snapshot{Timestamp: now, Price: 1005}, history)

	if got.BreakoutStrength == nil || *got.BreakoutStrength != 0 {
		t.Errorf("BreakoutStrength = %v, want 0 (a genuine no-breakout, not nil)", got.BreakoutStrength)
	}
}

func TestComputeScreenSignals_BreakoutIgnoresBarsOlderThanWindowAndFuture(t *testing.T) {
	now := time.Date(2026, 9, 27, 9, 30, 0, 0, time.UTC)
	history := []domain.Snapshot{
		{Timestamp: minutesAgo(now, 10), Price: 5000}, // outside the 5m window
		{Timestamp: minutesAgo(now, 2), Price: 1000},
		{Timestamp: now.Add(time.Minute), Price: 9000}, // future bar (FR-FE-1)
	}

	got := featureengine.ComputeScreenSignals(domain.Snapshot{Timestamp: now, Price: 1010}, history)

	if got.BreakoutStrength == nil {
		t.Fatal("BreakoutStrength = nil, want computed from the in-window bar")
	}
	if want := 1010.0/1000.0 - 1; !approxEqual(*got.BreakoutStrength, want) {
		t.Errorf("BreakoutStrength = %v, want %v", *got.BreakoutStrength, want)
	}
}

func TestComputeScreenSignals_NilWithoutHistory(t *testing.T) {
	now := time.Date(2026, 9, 27, 9, 30, 0, 0, time.UTC)

	got := featureengine.ComputeScreenSignals(domain.Snapshot{Timestamp: now, Price: 1000}, nil)

	if got.BreakoutStrength != nil || got.VolatilityExpansion != nil {
		t.Errorf("signals = (%v, %v), want both nil with no history", got.BreakoutStrength, got.VolatilityExpansion)
	}
}

func TestComputeScreenSignals_VolatilityExpansionIsShortOverLongRealizedVol(t *testing.T) {
	now := time.Date(2026, 9, 27, 9, 30, 0, 0, time.UTC)
	// Alternating +-0.1% bars, with the most recent swings widened to
	// +-3%: the 5-minute realized vol then exceeds the 15-minute one.
	prices := []float64{
		1000, 1001, 1000, 1001, 1000, 1001, 1000, 1001, 1000, 1001,
		1000, 1001, 1000, 1001, 1000, 1001, 1000, 1001, 1000, 1001,
	}
	history := minuteBars(now, prices...)
	history[len(history)-1].Price = 1030
	history[len(history)-2].Price = 990
	history[len(history)-3].Price = 1030

	got := featureengine.ComputeScreenSignals(domain.Snapshot{Timestamp: now, Price: 990}, history)

	if got.VolatilityExpansion == nil {
		t.Fatal("VolatilityExpansion = nil, want realized_vol_5m / realized_vol_15m")
	}
	if *got.VolatilityExpansion <= 1 {
		t.Errorf("VolatilityExpansion = %v, want > 1 when the last 5 minutes are choppier than the trailing 15", *got.VolatilityExpansion)
	}
}

func TestComputeScreenSignals_VolatilityExpansionNilWhenLongVolIsZero(t *testing.T) {
	now := time.Date(2026, 9, 27, 9, 30, 0, 0, time.UTC)
	history := minuteBars(now, 1000, 1000, 1000, 1000, 1000, 1000, 1000, 1000)

	got := featureengine.ComputeScreenSignals(domain.Snapshot{Timestamp: now, Price: 1000}, history)

	if got.VolatilityExpansion != nil {
		t.Errorf("VolatilityExpansion = %v, want nil (0/0 is not a ratio)", *got.VolatilityExpansion)
	}
}
