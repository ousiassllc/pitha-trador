package featureengine

import (
	"math"
	"time"
)

// atr1mBars / atr5mBars are how many trailing 1-minute / 5-minute bars
// the ATR averages true range over. The market_snapshots feed is a
// ~60-second sample rather than an OHLC tape, so bars are built from the
// sampled prices (see atr) and the periods are kept short enough for the
// HistoryLookbackBars window.
const (
	atr1mBars = 5
	atr5mBars = 3
)

// realizedVol is the sample standard deviation of up to minutes
// consecutive 1-minute returns ending at at, using the closest available
// bar at or before each minute mark to tolerate small gaps. nil unless at
// least two such returns can be computed.
func realizedVol(series []point, at time.Time, minutes int) *float64 {
	var returns []float64
	for k := range minutes {
		newer, ok1 := atOrBefore(series, at.Add(-time.Duration(k)*time.Minute))
		older, ok2 := atOrBefore(series, at.Add(-time.Duration(k+1)*time.Minute))
		if !ok1 || !ok2 || older.price == 0 || !newer.ts.After(older.ts) {
			continue
		}
		returns = append(returns, newer.price/older.price-1)
	}
	if len(returns) < 2 {
		return nil
	}
	v := sampleStdev(returns)
	return &v
}

// atr is the average true range, in price units, of up to n trailing
// bars of the given width ending at at. Bar k covers (at-(k+1)*width,
// at-k*width]; its high/low/close are the highest/lowest/last sampled
// price inside it (the current bar is the newest sample), and true
// range is max(high-low, |high-prevClose|,
// |low-prevClose|) against the previous non-empty bar's close. Empty
// buckets are skipped. nil unless at least one true range (i.e. two
// non-empty consecutive buckets) can be formed.
func atr(series []point, at time.Time, width time.Duration, n int) *float64 {
	type bar struct{ high, low, close float64 }
	bars := make([]bar, 0, n+1) // newest first
	for k := 0; k <= n; k++ {
		end := at.Add(-time.Duration(k) * width)
		var b bar
		found := false
		for _, p := range inWindow(series, end, width) {
			if p.price <= 0 {
				continue
			}
			if !found {
				b = bar{high: p.price, low: p.price, close: p.price}
				found = true
				continue
			}
			b.high, b.low, b.close = max(b.high, p.price), min(b.low, p.price), p.price
		}
		if found {
			bars = append(bars, b)
		}
	}

	var trs []float64
	for i := 0; i+1 < len(bars) && len(trs) < n; i++ {
		prevClose := bars[i+1].close
		tr := max(bars[i].high-bars[i].low, math.Abs(bars[i].high-prevClose), math.Abs(bars[i].low-prevClose))
		trs = append(trs, tr)
	}
	if len(trs) == 0 {
		return nil
	}
	v := mean(trs)
	return &v
}
