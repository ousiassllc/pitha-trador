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
