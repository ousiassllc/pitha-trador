package labelcompare

import (
	"math"
	"slices"

	"gonum.org/v1/gonum/stat/distuv"

	"github.com/ousiassllc/pitha-trador/internal/service/actuals"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/experiment"
)

// dayMeans are the per-day means of s in day order (the observations of the
// statistics below: the day is the unit of analysis) and their day keys.
func dayMeans(s series) (means []float64, keys []int64) {
	keys = make([]int64, 0, len(s))
	for k, d := range s {
		if d.n > 0 {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	means = make([]float64, len(keys))
	for i, k := range keys {
		means[i] = s[k].sum / float64(s[k].n)
	}
	return means, keys
}

// statOf is the Stat of s: the mean of the per-day means with the day-clustered
// 95% interval of actuals.ClusterMean (each day is one cluster holding its
// mean, so this is the t interval over the days).
func statOf(s series) Stat {
	means, keys := dayMeans(s)
	st := Stat{Days: len(keys)}
	for _, k := range keys {
		st.N += s[k].n
	}
	if len(keys) == 0 {
		return st
	}
	m := actuals.ClusterMean(means, keys)
	if !finite(m.Value) {
		return st
	}
	st.MeanNetBps = &m.Value
	if m.HasCI && finite(m.Lo) && finite(m.Hi) {
		st.CILow, st.CIHigh = &m.Lo, &m.Hi
	}
	return st
}

// compare is the SideCompare of one series in the two windows. The difference
// is Welch's interval on the two windows' per-day means (the windows are
// separate periods, so independent samples of days), after minus before.
func compare(before, after series) SideCompare {
	c := SideCompare{Before: statOf(before), After: statOf(after)}
	b, _ := dayMeans(before)
	a, _ := dayMeans(after)
	t := experiment.Welch(b, a)
	if t.Diff == nil {
		return c
	}
	c.Diff = &Diff{DiffBps: *t.Diff, CILow: t.CILow, CIHigh: t.CIHigh, T: t.T, DF: t.DF}
	return c
}

// variance is the sample variance of xs (n - 1); ok is false with fewer than
// two values.
func variance(xs []float64) (v float64, ok bool) {
	if len(xs) < 2 {
		return 0, false
	}
	var sum float64
	for _, x := range xs {
		sum += x
	}
	mean := sum / float64(len(xs))
	var ss float64
	for _, x := range xs {
		ss += (x - mean) * (x - mean)
	}
	return ss / float64(len(xs)-1), true
}

// daysNeeded is the smallest number of days g per window at which the 95%
// half-width of the difference, t(2g-2) * sqrt((var_before + var_after) / g),
// is below |diff| at the current variances (equal-sized windows; the t quantile
// at 2g-2 degrees of freedom is the equal-size Welch value). ok is false when
// no g up to MaxDaysNeeded does.
func daysNeeded(varBefore, varAfter, diff float64) (g int, ok bool) {
	spread := varBefore + varAfter
	for g = 2; g <= MaxDaysNeeded; g++ {
		half := distuv.StudentsT{Mu: 0, Sigma: 1, Nu: float64(2*g - 2)}.Quantile(1-(1-experiment.CILevel)/2) * math.Sqrt(spread/float64(g))
		if half < math.Abs(diff) {
			return g, true
		}
	}
	return 0, false
}

func finite(x float64) bool { return !math.IsNaN(x) && !math.IsInf(x, 0) }
