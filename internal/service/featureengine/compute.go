package featureengine

import (
	"math"
	"sort"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// Reading is the current-cycle raw market data point for one instrument,
// as reported by internal/service/marketdata. It is a separate type
// (rather than importing marketdata.Board directly) so this package keeps
// depending only on internal/domain and internal/repository (doc.go).
// Callers translate marketdata.Board into a Reading.
type Reading struct {
	Price    float64
	VWAP     float64
	Volume   int64
	Turnover float64

	// Bid, Ask, BidQty and AskQty are nil whenever kabuステーションAPI
	// does not report board data for this instrument/time, in which case
	// every feature derived from them (OrderbookImbalance, and the
	// snapshot-level SpreadBps built in engine.go) is nil rather than a
	// placeholder number (FR-FE-2).
	Bid    *float64
	Ask    *float64
	BidQty *float64
	AskQty *float64
}

// HistoryLookbackBars is how many prior market_snapshots bars the live
// market-data job passes as Input.History for each new bar. Compute's
// longest window is 15 minutes (Return15m); at the 60s full-scan cadence
// 20 bars covers that with margin. internal/service/backtest's
// VerifyNoLookahead recomputes every bar from the same bounded history,
// since VolumeRatio5m's baseline averages over every history bar
// supplied and so depends on exactly how many there were.
const HistoryLookbackBars = 20

// Input bundles everything Compute needs to derive one instrument's
// Feature values for the bar at Timestamp.
type Input struct {
	Timestamp time.Time
	Current   Reading

	// History is this instrument's prior market_snapshots rows, in any
	// order. Bars timestamped after Timestamp are ignored so a caller
	// that accidentally passes "future" bars (e.g. a backtest replaying a
	// full day at once) cannot leak look-ahead information (FR-FE-1).
	History []domain.Snapshot

	// MarketReturn5m and SectorReturn5m are computed by the caller from
	// TOPIX/Nikkei225 and sector index instruments' own histories -
	// using ReturnOverWindow the same way a stock's Return5m is derived
	// below - and passed through unchanged. nil when unavailable.
	MarketReturn5m *float64
	SectorReturn5m *float64
}

// Compute derives one instrument's Feature values for the bar at
// in.Timestamp (functional.md §4.1). Every return/ratio/volatility value
// uses only in.Current and in.History bars timestamped at or before
// in.Timestamp (FR-FE-1); OrderbookImbalance is nil whenever in.Current
// lacks bid/ask quantities (FR-FE-2).
func Compute(in Input) domain.Feature {
	series := buildSeries(in)

	return domain.Feature{
		Return1m:  returnOverWindow(series, in.Timestamp, in.Current.Price, time.Minute),
		Return5m:  returnOverWindow(series, in.Timestamp, in.Current.Price, 5*time.Minute),
		Return15m: returnOverWindow(series, in.Timestamp, in.Current.Price, 15*time.Minute),

		VWAP:           in.Current.VWAP,
		PriceVsVWAPBps: priceVsVWAPBps(in.Current.Price, in.Current.VWAP),

		VolumeRatio5m:      volumeRatio5m(series, in.Timestamp, in.Current.Volume),
		OrderbookImbalance: orderbookImbalance(in.Current),
		RealizedVol5m:      realizedVol5m(series, in.Timestamp),
		MarketReturn5m:     in.MarketReturn5m,
		SectorReturn5m:     in.SectorReturn5m,
	}
}

// ReturnOverWindow computes (currentPrice / referencePrice) - 1, where
// referencePrice is the Price of the most recent history bar timestamped
// at or before at.Add(-window), or nil if no such bar exists. It is
// exported so callers can derive MarketReturn5m/SectorReturn5m for
// TOPIX/Nikkei225/sector index instruments using the exact same
// look-ahead-safe logic Compute uses for a stock's own returns.
func ReturnOverWindow(at time.Time, currentPrice float64, history []domain.Snapshot, window time.Duration) *float64 {
	series := buildSeries(Input{Timestamp: at, Current: Reading{Price: currentPrice}, History: history})
	return returnOverWindow(series, at, currentPrice, window)
}

// point is one instant's price/volume, drawn from either a persisted
// history bar or the current reading, used to answer "what was the
// price/volume at-or-before time T" look-ahead-safe queries.
type point struct {
	ts     time.Time
	price  float64
	volume int64
}

// buildSeries merges in.History (dropping any bar after in.Timestamp,
// FR-FE-1) with the current reading into one ascending-by-time series.
func buildSeries(in Input) []point {
	pts := make([]point, 0, len(in.History)+1)
	for _, h := range in.History {
		if h.Timestamp.After(in.Timestamp) {
			continue
		}
		pts = append(pts, point{ts: h.Timestamp, price: h.Price, volume: h.Volume})
	}
	pts = append(pts, point{ts: in.Timestamp, price: in.Current.Price, volume: in.Current.Volume})

	sort.Slice(pts, func(i, j int) bool { return pts[i].ts.Before(pts[j].ts) })
	return pts
}

// atOrBefore returns the latest point in series (ascending by ts) with
// ts <= at, or false if none exists.
func atOrBefore(series []point, at time.Time) (point, bool) {
	var best point
	found := false
	for _, p := range series {
		if p.ts.After(at) {
			break
		}
		best, found = p, true
	}
	return best, found
}

func returnOverWindow(series []point, at time.Time, currentPrice float64, window time.Duration) *float64 {
	ref, ok := atOrBefore(series, at.Add(-window))
	if !ok || ref.price == 0 {
		return nil
	}
	v := currentPrice/ref.price - 1
	return &v
}

func priceVsVWAPBps(price, vwap float64) float64 {
	if vwap == 0 {
		return 0
	}
	return (price - vwap) / vwap * 10000
}

// volumeDelta returns currentVolume minus the cumulative session volume
// window earlier than at (i.e. this instrument's traded volume over the
// trailing window ending at at), or false if no bar exists that far back.
func volumeDelta(series []point, at time.Time, currentVolume int64, window time.Duration) (float64, bool) {
	ref, ok := atOrBefore(series, at.Add(-window))
	if !ok {
		return 0, false
	}
	return float64(currentVolume - ref.volume), true
}

// volumeRatio5m compares the trailing 5-minute traded volume ending at at
// against the average trailing-5-minute volume sampled at every earlier
// bar in series, using the same volumeDelta calculation for each sample.
// nil if either the recent window or every historical sample is
// unavailable (insufficient history).
func volumeRatio5m(series []point, at time.Time, currentVolume int64) *float64 {
	recent, ok := volumeDelta(series, at, currentVolume, 5*time.Minute)
	if !ok {
		return nil
	}

	var baseline []float64
	for _, p := range series {
		if !p.ts.Before(at) {
			continue
		}
		if delta, ok := volumeDelta(series, p.ts, p.volume, 5*time.Minute); ok {
			baseline = append(baseline, delta)
		}
	}
	if len(baseline) == 0 {
		return nil
	}

	avg := mean(baseline)
	if avg == 0 {
		return nil
	}
	ratio := recent / avg
	return &ratio
}

// orderbookImbalance is (bidQty-askQty)/(bidQty+askQty), or nil whenever
// either quantity is missing (FR-FE-2) or both are zero.
func orderbookImbalance(r Reading) *float64 {
	if r.BidQty == nil || r.AskQty == nil {
		return nil
	}
	total := *r.BidQty + *r.AskQty
	if total == 0 {
		return nil
	}
	v := (*r.BidQty - *r.AskQty) / total
	return &v
}

// realizedVol5m is the sample standard deviation of up to five
// consecutive 1-minute returns ending at at (see realizedVol).
func realizedVol5m(series []point, at time.Time) *float64 {
	return realizedVol(series, at, 5)
}

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

func mean(xs []float64) float64 {
	var sum float64
	for _, x := range xs {
		sum += x
	}
	return sum / float64(len(xs))
}

func sampleStdev(xs []float64) float64 {
	m := mean(xs)
	var sumSq float64
	for _, x := range xs {
		d := x - m
		sumSq += d * d
	}
	return math.Sqrt(sumSq / float64(len(xs)-1))
}
