package featureengine

import (
	"math"
	"sort"
	"time"
)

// point is one instant's raw values, drawn from either a persisted
// history bar or the current reading, used to answer "what was the
// value at-or-before time T" look-ahead-safe queries.
type point struct {
	ts       time.Time
	price    float64
	vwap     float64
	volume   int64
	turnover float64
}

// buildSeries merges in.History (dropping any bar after in.Timestamp,
// FR-FE-1) with the current reading into one ascending-by-time series.
func buildSeries(in Input) []point {
	pts := make([]point, 0, len(in.History)+1)
	for _, h := range in.History {
		if h.Timestamp.After(in.Timestamp) {
			continue
		}
		pts = append(pts, point{ts: h.Timestamp, price: h.Price, vwap: h.Feature.VWAP, volume: h.Volume, turnover: h.Turnover})
	}
	pts = append(pts, point{
		ts: in.Timestamp, price: in.Current.Price, vwap: in.Current.VWAP,
		volume: in.Current.Volume, turnover: in.Current.Turnover,
	})

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

// minRefTolerance is the least a window's reference bar may lag the
// window start. Bars are ~60s samples, so one missed/late bar (or a few
// seconds of scan jitter) must not blank every short-window feature.
const minRefTolerance = 90 * time.Second

// refTolerance is how far before the window start (at-window) a
// reference bar may sit and still be "the value window ago": half the
// window, but at least minRefTolerance.
func refTolerance(window time.Duration) time.Duration {
	return max(window/2, minRefTolerance)
}

// freshAtOrBefore is atOrBefore that also rejects a point older than
// tolerance before t, so a stale bar is never mistaken for the value at t.
func freshAtOrBefore(series []point, t time.Time, tolerance time.Duration) (point, bool) {
	p, ok := atOrBefore(series, t)
	if !ok || t.Sub(p.ts) > tolerance {
		return point{}, false
	}
	return p, true
}

// windowRef is the reference bar for the trailing window ending at at:
// the latest bar at or before at-window that is no more than
// refTolerance(window) older than at-window. A gap larger than that
// (previous session's bar at the open, pre-lunch bar after 12:30, a
// restart or outage) is missing history, not an N-minute move: the
// caller reports nil instead of an overnight/lunch gap as momentum
// (FR-FE-1/FR-FE-2, issues #166/#176).
func windowRef(series []point, at time.Time, window time.Duration) (point, bool) {
	return freshAtOrBefore(series, at.Add(-window), refTolerance(window))
}

// inWindow returns the points with at-window < ts <= at, in ascending
// order.
func inWindow(series []point, at time.Time, window time.Duration) []point {
	from := at.Add(-window)
	var out []point
	for _, p := range series {
		if p.ts.After(from) && !p.ts.After(at) {
			out = append(out, p)
		}
	}
	return out
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

// ratioOrNil is num/den, or nil if either is nil or den is zero.
func ratioOrNil(num, den *float64) *float64 {
	if num == nil || den == nil || *den == 0 {
		return nil
	}
	v := *num / *den
	return &v
}

// difference is a-b, or nil if either is nil.
func difference(a, b *float64) *float64 {
	if a == nil || b == nil {
		return nil
	}
	v := *a - *b
	return &v
}
