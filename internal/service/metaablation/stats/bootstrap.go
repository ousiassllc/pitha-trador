package stats

import (
	"math"
	"math/rand/v2"
	"slices"
)

// Cluster is one resampling unit (a trading day): the sum of the per-unit
// values and the number of units in it. The statistic of a set of clusters is
// sum(Sum) / sum(N): a mean per eligible candidate (N = candidates that day) or
// a mean of daily values (N = 1).
type Cluster struct {
	Sum, N float64
}

// Interval is a percentile interval.
type Interval struct {
	Lower, Upper float64
}

// Contains reports whether v lies inside the closed interval.
func (i Interval) Contains(v float64) bool { return i.Lower <= v && v <= i.Upper }

// Estimate is the statistic of the clusters with its day-clustered bootstrap
// interval.
type Estimate struct {
	// Defined is false when no unit exists (the mean is undefined).
	Defined bool
	Mean    float64
	// Days is the number of clusters (trading days) and Units the number of
	// units (sum of N).
	Days  int
	Units float64
	// CI is nil with fewer than two days or no defined draw.
	CI *Interval
	// SE is the bootstrap standard deviation of the statistic (0 without CI).
	SE float64
	// Level is 1 - the two-sided alpha the interval was drawn at.
	Level     float64
	Resamples int
}

// Mean is sum(Sum) / sum(N) over the clusters; ok is false with no units.
func Mean(clusters []Cluster) (mean float64, ok bool) {
	var s, n float64
	for _, c := range clusters {
		s += c.Sum
		n += c.N
	}
	if n <= 0 {
		return 0, false
	}
	return s / n, true
}

// Describe is the statistic of the clusters without an interval: what a
// reference comparison (a diagonal) reports. It is not a test, so it carries no
// CI, no detectable difference and no verdict, and is not a member of the
// multiple-comparison family.
func Describe(clusters []Cluster) Estimate {
	est := Estimate{Days: len(clusters)}
	for _, c := range clusters {
		est.Units += c.N
	}
	est.Mean, est.Defined = Mean(clusters)
	return est
}

// Bootstrap resamples the clusters (whole days, with replacement) resamples
// times and returns the percentile interval at the two-sided level alpha. It is
// deterministic for a given seed. Draws without a unit are skipped.
func Bootstrap(clusters []Cluster, resamples int, alpha float64, seed uint64) Estimate {
	est := Estimate{Days: len(clusters), Level: 1 - alpha, Resamples: resamples}
	for _, c := range clusters {
		est.Units += c.N
	}
	est.Mean, est.Defined = Mean(clusters)
	if !est.Defined || len(clusters) < 2 {
		return est
	}
	rng := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15)) //nolint:gosec // G404: a reproducible resampling stream, not a secret
	draws := make([]float64, 0, resamples)
	for range resamples {
		var s, n float64
		for range clusters {
			c := clusters[rng.IntN(len(clusters))]
			s += c.Sum
			n += c.N
		}
		if n > 0 {
			draws = append(draws, s/n)
		}
	}
	if len(draws) < 2 {
		return est
	}
	slices.Sort(draws)
	est.CI = &Interval{Lower: quantile(draws, alpha/2), Upper: quantile(draws, 1-alpha/2)}
	var sum float64
	for _, d := range draws {
		sum += d
	}
	mean := sum / float64(len(draws))
	var ss float64
	for _, d := range draws {
		ss += (d - mean) * (d - mean)
	}
	est.SE = math.Sqrt(ss / float64(len(draws)-1))
	return est
}

// quantile is the nearest-rank quantile of the sorted draws, the lower tail
// rounded down and the upper tail rounded up (the wider, conservative interval).
func quantile(sorted []float64, q float64) float64 {
	n := float64(len(sorted))
	var i int
	if q < 0.5 {
		i = int(math.Floor(q * n))
	} else {
		i = int(math.Ceil(q*n)) - 1
	}
	return sorted[max(0, min(len(sorted)-1, i))]
}
