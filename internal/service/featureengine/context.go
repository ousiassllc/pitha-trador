package featureengine

import (
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// IndexReturn is the window return of one tracked index instrument
// (instruments.kind = market_index / sector_index) as of at: its latest
// bar timestamped at or before at, measured against its own history with
// the same look-ahead-safe ReturnOverWindow a stock's returns use. bars
// are that index's market_snapshots rows in any order. nil when there is
// no such bar, the latest bar is older than maxStale (a stalled index
// feed must not be read as the current market), or the history does not
// reach back window (FR-FE-1/FR-FE-2).
func IndexReturn(at time.Time, bars []domain.Snapshot, window, maxStale time.Duration) *float64 {
	var latest *domain.Snapshot
	for i := range bars {
		if bars[i].Timestamp.After(at) {
			continue
		}
		if latest == nil || bars[i].Timestamp.After(latest.Timestamp) {
			latest = &bars[i]
		}
	}
	if latest == nil || at.Sub(latest.Timestamp) > maxStale {
		return nil
	}
	return ReturnOverWindow(latest.Timestamp, latest.Price, bars, window)
}

// MeanReturn is the arithmetic mean of the non-nil returns, or nil when
// there are none. It combines several market indices (TOPIX, Nikkei225)
// into one market_return value.
func MeanReturn(returns []*float64) *float64 {
	var sum float64
	var n int
	for _, r := range returns {
		if r != nil {
			sum += *r
			n++
		}
	}
	if n == 0 {
		return nil
	}
	v := sum / float64(n)
	return &v
}

// Breadth is market_breadth: (advancers - decliners) / count over the
// stock universe's latest 5-minute returns, in [-1, 1]. nil when returns
// is empty. Unchanged stocks count toward the total only.
func Breadth(returns []float64) *float64 {
	if len(returns) == 0 {
		return nil
	}
	var adv, dec int
	for _, r := range returns {
		switch {
		case r > 0:
			adv++
		case r < 0:
			dec++
		}
	}
	v := float64(adv-dec) / float64(len(returns))
	return &v
}
