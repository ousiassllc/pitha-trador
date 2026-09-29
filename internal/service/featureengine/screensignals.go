package featureengine

import (
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// breakoutWindow is the prior-range lookback for BreakoutStrength: the
// same 5-minute horizon as functional.md §4.1's high_distance_5m /
// low_distance_5m.
const breakoutWindow = 5 * time.Minute

// ScreenSignals are the two Fast Screener screen_score inputs
// (functional.md §4.2, FR-FS-2) that are derived from a bar's price
// history rather than read off domain.Feature: breakout_strength and
// volatility_expansion. Like Feature's own nullable fields, nil means
// "not computable from the available history" and makes
// screener.ScreenScore drop the term instead of scoring it as zero.
type ScreenSignals struct {
	BreakoutStrength    *float64
	VolatilityExpansion *float64
}

// ComputeScreenSignals derives ScreenSignals for current from history
// (current's prior market_snapshots bars, in any order). Like Compute,
// only bars timestamped at or before current.Timestamp are used
// (FR-FE-1).
//
//   - BreakoutStrength is how far current.Price sits beyond the high or
//     low of the preceding 5 minutes' bars (excluding current itself),
//     as a non-negative fraction of that extreme: price/high - 1 above
//     the range, 1 - price/low below it (the magnitude of a
//     high_distance_5m/low_distance_5m break), and 0 while price is
//     inside the range. nil without at least one bar in that window.
//   - VolatilityExpansion is volatility_expansion_ratio (§4.1):
//     realized_vol_5m / realized_vol_15m. nil unless both are computable
//     and the 15-minute value is non-zero.
func ComputeScreenSignals(current domain.Snapshot, history []domain.Snapshot) ScreenSignals {
	series := buildSeries(Input{
		Timestamp: current.Timestamp,
		Current:   Reading{Price: current.Price, Volume: current.Volume},
		History:   history,
	})
	return ScreenSignals{
		BreakoutStrength:    breakoutStrength(series, current.Timestamp, current.Price),
		VolatilityExpansion: volatilityExpansionRatio(series, current.Timestamp),
	}
}

func breakoutStrength(series []point, at time.Time, price float64) *float64 {
	var high, low float64
	found := false
	for _, p := range series {
		if !p.ts.Before(at) || p.ts.Before(at.Add(-breakoutWindow)) || p.price <= 0 {
			continue
		}
		if !found {
			high, low, found = p.price, p.price, true
			continue
		}
		high = max(high, p.price)
		low = min(low, p.price)
	}
	if !found || price <= 0 {
		return nil
	}

	var v float64
	switch {
	case price > high:
		v = price/high - 1
	case price < low:
		v = 1 - price/low
	}
	return &v
}

func volatilityExpansionRatio(series []point, at time.Time) *float64 {
	short := realizedVol(series, at, 5)
	long := realizedVol(series, at, 15)
	if short == nil || long == nil || *long == 0 {
		return nil
	}
	v := *short / *long
	return &v
}
