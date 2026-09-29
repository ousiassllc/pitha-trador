package backtest

import (
	"fmt"
	"math"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/featureengine"
)

// LookaheadViolation records one bar whose persisted Feature value could
// not have been produced using only data at or before its own Timestamp
// (FR-BT-3): recomputing purely from the bars before it in the same
// series produced a different result.
type LookaheadViolation struct {
	Index      int
	Timestamp  time.Time
	Field      string
	Stored     *float64
	Recomputed *float64
}

// String formats v for inclusion in an error message.
func (v LookaheadViolation) String() string {
	return fmt.Sprintf("bar %d (%s): %s stored=%s recomputed=%s",
		v.Index, v.Timestamp.Format(time.RFC3339), v.Field, formatFloatPtr(v.Stored), formatFloatPtr(v.Recomputed))
}

// VerifyNoLookahead checks every bar in bars[warmup:] - bars must already
// be sorted ascending by Timestamp - against featureengine.Compute's own
// look-ahead-safe logic (FR-FE-1), recomputing each bar's
// history-dependent Feature values (Return1m/3m/5m/15m/30m,
// VolumeRatio1m/5m, Turnover5m, RealizedVol5m/15m,
// VolatilityExpansionRatio) from the featureengine.HistoryLookbackBars
// bars before it (the same bounded history the live market-data job
// supplies), and reports every bar whose persisted value disagrees
// (FR-BT-3). The leading warmup bars are history only: their own Feature
// values were computed from bars before the slice, so they cannot be
// recomputed from it.
//
// VWAP/PriceVsVWAPBps (derived from the current bar alone, no history
// dependency) and the board-depth, session high/low and market-context
// features (board data and other-instrument history that
// domain.Snapshot does not retain, so this package cannot recompute them
// from bars alone) are outside this check's scope: none of them can leak
// look-ahead information through bars, since none of them depend on
// which later bars happen to be present in the slice.
func VerifyNoLookahead(bars []domain.Snapshot, warmup int) []LookaheadViolation {
	var violations []LookaheadViolation
	for i := max(warmup, 0); i < len(bars); i++ {
		bar := bars[i]
		recomputed := featureengine.Compute(featureengine.Input{
			Timestamp: bar.Timestamp,
			Current:   readingFromSnapshot(bar),
			History:   bars[max(0, i-featureengine.HistoryLookbackBars):i],
		})

		checks := [...]struct {
			field              string
			stored, recomputed *float64
		}{
			{"Return1m", bar.Feature.Return1m, recomputed.Return1m},
			{"Return5m", bar.Feature.Return5m, recomputed.Return5m},
			{"Return15m", bar.Feature.Return15m, recomputed.Return15m},
			{"Return3m", bar.Feature.Return3m, recomputed.Return3m},
			{"Return30m", bar.Feature.Return30m, recomputed.Return30m},
			{"VolumeRatio1m", bar.Feature.VolumeRatio1m, recomputed.VolumeRatio1m},
			{"VolumeRatio5m", bar.Feature.VolumeRatio5m, recomputed.VolumeRatio5m},
			{"Turnover5m", bar.Feature.Turnover5m, recomputed.Turnover5m},
			{"RealizedVol5m", bar.Feature.RealizedVol5m, recomputed.RealizedVol5m},
			{"RealizedVol15m", bar.Feature.RealizedVol15m, recomputed.RealizedVol15m},
			{"VolatilityExpansionRatio", bar.Feature.VolatilityExpansionRatio, recomputed.VolatilityExpansionRatio},
		}
		for _, c := range checks {
			if !floatPtrEqual(c.stored, c.recomputed) {
				violations = append(violations, LookaheadViolation{
					Index: i, Timestamp: bar.Timestamp, Field: c.field,
					Stored: c.stored, Recomputed: c.recomputed,
				})
			}
		}
	}
	return violations
}

// readingFromSnapshot converts a persisted domain.Snapshot back into the
// featureengine.Reading Compute needs to recompute its Feature. BidQty/
// AskQty are always nil: domain.Snapshot only retains the derived
// OrderbookImbalance, not the raw board quantities Compute would need to
// reproduce it (see VerifyNoLookahead's doc comment on why that field is
// out of scope for this check).
func readingFromSnapshot(s domain.Snapshot) featureengine.Reading {
	return featureengine.Reading{
		Price:    s.Price,
		VWAP:     s.Feature.VWAP,
		Volume:   s.Volume,
		Turnover: s.Turnover,
		Bid:      s.Bid,
		Ask:      s.Ask,
	}
}

// floatPtrEqual reports whether a and b are both nil, or both non-nil and
// equal within a small floating-point tolerance.
func floatPtrEqual(a, b *float64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return math.Abs(*a-*b) < 1e-9
}

// formatFloatPtr formats a *float64 for LookaheadViolation.String, using
// "nil" rather than a dereference-panic when the pointer is nil.
func formatFloatPtr(f *float64) string {
	if f == nil {
		return "nil"
	}
	return fmt.Sprintf("%v", *f)
}
