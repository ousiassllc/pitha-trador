package evaldiag

import (
	"math"
	"slices"

	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/stats"
)

// Indexes of the arms in evalRow.net, in the order of Arms.
const (
	iLLM = iota
	iRandom
	iMomentum
	iContrarian
	iNone
	iLong
	iShort
	numArms
)

// evalRow is one evaluated decision: its JST day, its own direction, the net
// return (bps) of every arm on it, whether it entered, and the stored label.
type evalRow struct {
	day     string
	dir     string
	net     [numArms]float64
	entered bool
	stored  *float64
}

// dayAgg is one JST day's sum, count and wins of a series.
type dayAgg struct {
	day  string
	sum  float64
	n    int
	wins int
}

// byDay aggregates value over rows per JST day, ascending.
func byDay(rows []evalRow, value func(evalRow) float64) []dayAgg {
	index := map[string]*dayAgg{}
	var out []*dayAgg
	for _, r := range rows {
		a := index[r.day]
		if a == nil {
			a = &dayAgg{day: r.day}
			index[r.day] = a
			out = append(out, a)
		}
		v := value(r)
		a.sum += v
		a.n++
		if v > 0 {
			a.wins++
		}
	}
	slices.SortFunc(out, func(x, y *dayAgg) int {
		switch {
		case x.day < y.day:
			return -1
		case x.day > y.day:
			return 1
		}
		return 0
	})
	res := make([]dayAgg, len(out))
	for i, a := range out {
		res[i] = *a
	}
	return res
}

func clustersOf(days []dayAgg) []stats.Cluster {
	cs := make([]stats.Cluster, len(days))
	for i, d := range days {
		cs[i] = stats.Cluster{Sum: d.sum, N: float64(d.n)}
	}
	return cs
}

func ciOf(est stats.Estimate) *CI {
	if est.CI == nil {
		return nil
	}
	return &CI{Lower: est.CI.Lower, Upper: est.CI.Upper, Level: est.Level}
}

// detectableMinimum is the smallest true mean the estimate's bootstrap standard
// error could tell from 0 at Alpha and Power; nil without an interval (fewer
// than two days).
func detectableMinimum(est stats.Estimate) *float64 {
	if est.CI == nil {
		return nil
	}
	m := stats.DetectableMinimum(Alpha, Power, est.SE)
	return &m
}

// daysToDetect answers, for every target, how many more days of the same kind
// are needed to detect it when the standard error shrinks with 1/sqrt(days):
// the minimum scales as 1/sqrt(days'), so days' = days * (minimum/target)^2 and
// the additional days are the ceiling of the difference (never negative). A
// minimum already at or below the target is AlreadyDetectable with 0 days; an
// undefined minimum (nil) leaves AdditionalDays null.
func daysToDetect(days int, minimum *float64) []DaysToDetect {
	out := make([]DaysToDetect, 0, len(TargetsBps))
	for _, target := range TargetsBps {
		d := DaysToDetect{TargetBps: target}
		if minimum != nil {
			extra := 0
			if *minimum <= target {
				d.AlreadyDetectable = true
			} else {
				ratio := *minimum / target
				// 1e-9 keeps an exact quotient (e.g. 10 days, minimum 6, target 3 -> 30)
				// from rounding up on float error.
				extra = max(0, int(math.Ceil(float64(days)*ratio*ratio-float64(days)-1e-9)))
			}
			d.AdditionalDays = &extra
		}
		out = append(out, d)
	}
	return out
}

// metricsOf summarizes the series value over rows: count, days, mean with its
// day-cluster bootstrap interval, win rate (value > 0 over rows) and the
// detectable minimum edge with the days still needed. daily adds the per-day
// rows.
func metricsOf(rows []evalRow, value func(evalRow) float64, daily bool) Metrics {
	m := Metrics{N: len(rows), DaysToDetect: []DaysToDetect{}}
	if len(rows) == 0 {
		return m
	}
	days := byDay(rows, value)
	est := stats.Bootstrap(clustersOf(days), Resamples, Alpha, Seed)
	wins := 0
	for _, d := range days {
		wins += d.wins
	}
	rate := float64(wins) / float64(len(rows))
	mean := est.Mean
	m.Days, m.MeanNetBps, m.CI, m.WinRate = len(days), &mean, ciOf(est), &rate
	m.DetectableMinimumEdgeBps = detectableMinimum(est)
	m.DaysToDetect = daysToDetect(len(days), m.DetectableMinimumEdgeBps)
	if daily {
		for _, d := range days {
			m.Daily = append(m.Daily, Day{Date: d.day, N: d.n, MeanNetBps: d.sum / float64(d.n), WinRate: float64(d.wins) / float64(d.n), TotalNetBps: d.sum})
		}
	}
	return m
}

// diffOf is the paired difference of arm a minus arm b on the same rows: a day's
// cluster is the sum of the row differences over the day's rows.
func diffOf(rows []evalRow, a, b int) Diff {
	d := Diff{N: len(rows), DaysToDetect: []DaysToDetect{}}
	if len(rows) == 0 {
		return d
	}
	days := byDay(rows, func(r evalRow) float64 { return r.net[a] - r.net[b] })
	est := stats.Bootstrap(clustersOf(days), Resamples, Alpha, Seed)
	mean := est.Mean
	d.Days, d.MeanDiffBps, d.CI = len(days), &mean, ciOf(est)
	d.DetectableMinimumDifferenceBps = detectableMinimum(est)
	d.DaysToDetect = daysToDetect(len(days), d.DetectableMinimumDifferenceBps)
	return d
}

// subsetOf returns the rows whose own direction is dir ("" for all).
func subsetOf(rows []evalRow, dir string) []evalRow {
	if dir == "" {
		return rows
	}
	var out []evalRow
	for _, r := range rows {
		if r.dir == dir {
			out = append(out, r)
		}
	}
	return out
}
