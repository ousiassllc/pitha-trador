package actuals

import (
	"math"
	"math/rand/v2"
	"slices"
	"time"
)

// jst is the Tokyo Stock Exchange's time zone: the day a trade or decision
// belongs to. Japan has no DST, so a fixed zone is exact and needs no tzdata.
var jst = time.FixedZone("JST", 9*60*60)

// jstDay numbers the JST calendar day containing t.
func jstDay(t time.Time) int64 {
	const day = 24 * 60 * 60
	s := t.Unix() + 9*60*60
	d := s / day
	if s%day < 0 {
		d--
	}
	return d
}

// tTable holds the two-sided 95% Student-t critical values for 1..30 degrees
// of freedom.
var tTable = [...]float64{
	12.706, 4.303, 3.182, 2.776, 2.571, 2.447, 2.365, 2.306, 2.262, 2.228,
	2.201, 2.179, 2.160, 2.145, 2.131, 2.120, 2.110, 2.101, 2.093, 2.086,
	2.080, 2.074, 2.069, 2.064, 2.060, 2.056, 2.052, 2.048, 2.045, 2.042,
}

// tCritical95 is the two-sided 95% Student-t critical value for df >= 1
// degrees of freedom (a step function beyond 30, rounded to the standard
// table rows 40/60/120/∞).
func tCritical95(df int) float64 {
	switch {
	case df <= len(tTable):
		return tTable[df-1]
	case df <= 40:
		return 2.021
	case df <= 60:
		return 2.000
	case df <= 120:
		return 1.980
	default:
		return 1.960
	}
}

// meanMetric is the mean of values with a 95% interval that is robust to
// correlation within a day: each distinct entry of days is one cluster, and
// the interval is the cluster-robust one, mean ± t(G-1) * SE with
//
//	SE = sqrt(G/(G-1) * Σ_g (Σ_{i in g} (x_i - mean))²) / n
//
// for G clusters of n observations. With a single cluster there is no
// interval (HasCI false).
func meanMetric(values []float64, days []int64, c Criteria) Metric {
	n := len(values)
	m := Metric{N: n}
	if n == 0 {
		return m
	}
	var sum float64
	for _, v := range values {
		sum += v
	}
	mean := sum / float64(n)
	m.Value, m.Defined = mean, true

	// Deviations summed per day, in day order so the result is bit-stable.
	byDay := map[int64]float64{}
	for i, v := range values {
		byDay[days[i]] += v - mean
	}
	keys := make([]int64, 0, len(byDay))
	for d := range byDay {
		keys = append(keys, d)
	}
	slices.Sort(keys)
	var ss float64
	for _, d := range keys {
		ss += byDay[d] * byDay[d]
	}
	g := len(keys)
	if g >= 2 {
		se := math.Sqrt(float64(g)/float64(g-1)*ss) / float64(n)
		half := tCritical95(g-1) * se
		m.Lo, m.Hi, m.HasCI = mean-half, mean+half, true
	}
	m.Reliable = m.HasCI && n >= c.MinSample && g >= c.MinClusters
	return m
}

// proportionMetric is meanMetric of 0/1 observations with the interval
// clamped to [0, 1].
func proportionMetric(values []float64, days []int64, c Criteria) Metric {
	m := meanMetric(values, days, c)
	if m.HasCI {
		m.Lo, m.Hi = max(m.Lo, 0), min(m.Hi, 1)
	}
	return m
}

// bootstrapResamples and the seeds make the bootstrap intervals reproducible:
// the same trades always give the same interval.
const (
	bootstrapResamples = 1000
	bootstrapSeedA     = 0x9e3779b97f4a7c15
	bootstrapSeedB     = 0xbf58476d1ce4e5b9
)

// dayBlocks splits pnl, ordered by time, into one block per run of equal
// days; blocks keep the within-day order.
func dayBlocks(pnl []float64, days []int64) [][]float64 {
	var blocks [][]float64
	for i, p := range pnl {
		if i == 0 || days[i] != days[i-1] {
			blocks = append(blocks, nil)
		}
		blocks[len(blocks)-1] = append(blocks[len(blocks)-1], p)
	}
	return blocks
}

// bootstrapMetric evaluates stat over pnl (ordered by time) and gives it a
// 95% percentile interval from a day-block bootstrap: whole days are redrawn
// with replacement, which keeps both the within-day correlation and the
// path (needed by drawdown). stat reports false when it is undefined for a
// sample; the interval needs at least half of the resamples to be defined.
func bootstrapMetric(pnl []float64, days []int64, stat func([]float64) (float64, bool), c Criteria) Metric {
	m := Metric{N: len(pnl)}
	if len(pnl) == 0 {
		return m
	}
	m.Value, m.Defined = stat(pnl)
	blocks := dayBlocks(pnl, days)
	g := len(blocks)
	if m.Defined && g >= 2 {
		// A fixed seed on purpose: reproducible intervals, nothing security related.
		rng := rand.New(rand.NewPCG(bootstrapSeedA, bootstrapSeedB)) //nolint:gosec // G404: statistical resampling, not a secret
		sample := make([]float64, 0, len(pnl))
		var vals []float64
		for range bootstrapResamples {
			sample = sample[:0]
			for range g {
				sample = append(sample, blocks[rng.IntN(g)]...)
			}
			if v, ok := stat(sample); ok {
				vals = append(vals, v)
			}
		}
		if k := len(vals); k >= bootstrapResamples/2 {
			slices.Sort(vals)
			m.Lo = vals[int(math.Floor(0.025*float64(k)))]
			m.Hi = vals[min(k-1, int(math.Ceil(0.975*float64(k)))-1)]
			m.HasCI = true
		}
	}
	m.Reliable = m.Defined && m.HasCI && len(pnl) >= c.MinSample && g >= c.MinClusters
	return m
}

// scaled multiplies a metric's figures by k > 0 (a unit change).
func scaled(m Metric, k float64) Metric {
	m.Value, m.Lo, m.Hi = m.Value*k, m.Lo*k, m.Hi*k
	return m
}
