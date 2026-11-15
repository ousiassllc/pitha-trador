package evaldiag

import (
	"slices"
	"strings"

	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/stats"
)

// Point is one observation of a series: the JST day (YYYY-MM-DD) it belongs to and its
// value (bps). The day is the bootstrap's cluster.
type Point struct {
	Day   string
	Value float64
}

// Summary is Metrics of a series (the same fixed Alpha, Power, Resamples and Seed as
// jev_evaluation) with what a multiple-comparison correction and a reader of a
// difference need besides: the bootstrap standard error and the two-sided p-value of
// "the true mean is 0" under a normal approximation. SE and P are nil with fewer than two
// days (the interval is undefined then too). Nothing here is a verdict.
type Summary struct {
	Metrics
	SE *float64 `json:"se_bps"`
	P  *float64 `json:"p_value"`
}

// DayTotals is one JST day of a pre-aggregated series: the number of observations, their sum
// (bps) and how many were above 0. The day is the bootstrap's cluster.
type DayTotals struct {
	Day  string
	N    int
	Sum  float64
	Wins int
}

// SummarizeDays summarizes a series that is already totalled per JST day (for example the rows
// of jev_evaluation_daily) exactly as Summarize does the series it was totalled from. Days with
// N == 0 are ignored; the per-day rows are always returned.
func SummarizeDays(days []DayTotals) Summary {
	var aggs []dayAgg
	for _, d := range days {
		if d.N > 0 {
			aggs = append(aggs, dayAgg{day: d.Day, sum: d.Sum, n: d.N, wins: d.Wins})
		}
	}
	return summarizeAggs(aggs, true)
}

// Summarize summarizes the series: count, days, mean with its day-cluster bootstrap
// interval, win rate (value > 0), the detectable minimum with the days still needed, SE
// and p. daily adds the per-day rows. A series of differences (candidate minus current on
// the same rows) is summarized the same way.
func Summarize(points []Point, daily bool) Summary {
	rows := make([]evalRow, len(points))
	for i, p := range points {
		rows[i] = evalRow{day: p.Day}
		rows[i].net[iLLM] = p.Value
	}
	return summarizeAggs(byDay(rows, func(r evalRow) float64 { return r.net[iLLM] }), daily)
}

// summarizeAggs is Summarize over the per-day aggregates (ascending days).
func summarizeAggs(days []dayAgg, daily bool) Summary {
	s := Summary{Metrics: Metrics{DaysToDetect: []DaysToDetect{}}}
	slices.SortFunc(days, func(a, b dayAgg) int { return strings.Compare(a.day, b.day) })
	for _, d := range days {
		s.N += d.n
	}
	if s.N == 0 {
		return s
	}
	est := stats.Bootstrap(clustersOf(days), Resamples, Alpha, Seed)
	wins := 0
	for _, d := range days {
		wins += d.wins
	}
	mean, rate := est.Mean, float64(wins)/float64(s.N)
	s.Days, s.MeanNetBps, s.CI, s.WinRate = len(days), &mean, ciOf(est), &rate
	s.DetectableMinimumEdgeBps = detectableMinimum(est)
	s.DaysToDetect = daysToDetect(len(days), s.DetectableMinimumEdgeBps)
	if daily {
		for _, d := range days {
			s.Daily = append(s.Daily, Day{Date: d.day, N: d.n, MeanNetBps: d.sum / float64(d.n), WinRate: float64(d.wins) / float64(d.n), TotalNetBps: d.sum})
		}
	}
	if est.CI == nil {
		return s
	}
	se := est.SE
	s.SE = &se
	if p, ok := stats.PValue(est.Mean, est.SE); ok {
		s.P = &p
	}
	return s
}
