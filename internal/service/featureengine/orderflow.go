package featureengine

import "time"

// tradeFlow classifies the volume traded across the trailing window by
// the tick rule and returns the buy share, sell share and
// (buy-sell)/(buy+sell). kabuステーションAPI's board carries no
// per-trade aggressor flag, so each consecutive pair of samples' volume
// increment is attributed to buyers when the price rose, to sellers when
// it fell, and to the previous non-flat direction when unchanged.
// Increments before any direction is known, and pairs whose cumulative
// volume went backwards, are unclassified. All three are nil unless some
// volume was classified (FR-FE-2).
func tradeFlow(series []point, at time.Time, window time.Duration) (buyRatio, sellRatio, imbalance *float64) {
	ref, ok := atOrBefore(series, at.Add(-window))
	if !ok {
		return nil, nil, nil
	}
	pts := append([]point{ref}, inWindow(series, at, window)...)

	var buy, sell float64
	dir := 0
	for i := 1; i < len(pts); i++ {
		prev, cur := pts[i-1], pts[i]
		switch {
		case cur.price > prev.price:
			dir = 1
		case cur.price < prev.price:
			dir = -1
		}
		delta := float64(cur.volume - prev.volume)
		if delta <= 0 {
			continue
		}
		switch dir {
		case 1:
			buy += delta
		case -1:
			sell += delta
		}
	}
	total := buy + sell
	if total == 0 {
		return nil, nil, nil
	}
	b, s, im := buy/total, sell/total, (buy-sell)/total
	return &b, &s, &im
}
