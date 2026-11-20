package labelcompare

import (
	"math"
	"slices"
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

// statOf is the Stat of s: the mean of the per-day means with the 95% interval
// of meanInterval (each day is one observation: the day is the cluster, and the
// correlation between neighbouring days is the Newey-West correction).
func statOf(s series) Stat {
	means, keys := dayMeans(s)
	st := Stat{Days: len(keys)}
	for _, k := range keys {
		st.N += s[k].n
	}
	if len(keys) == 0 {
		return st
	}
	m := meanOf(means)
	if !finite(m) {
		return st
	}
	st.MeanNetBps = &m
	st.CILow, st.CIHigh = meanInterval(means, NeweyWestLagDays)
	return st
}

// compare is the SideCompare of one series in the two windows: after minus
// before of the per-day means with diffOf's interval.
func compare(before, after series) SideCompare {
	b, _ := dayMeans(before)
	a, _ := dayMeans(after)
	return SideCompare{Before: statOf(before), After: statOf(after), Diff: diffOf(b, a, NeweyWestLagDays)}
}

// daysNeeded is the smallest number of days g per window at which the 95%
// half-width of the difference, t(2g-2) * sqrt((v_before + v_after) / g), is
// below |diff| at the current long-run variances (equal-sized windows; the t
// quantile at 2g-2 degrees of freedom is the equal-size Welch value). ok is
// false when no g up to MaxDaysNeeded does.
func daysNeeded(varBefore, varAfter, diff float64) (g int, ok bool) {
	spread := varBefore + varAfter
	for g = 2; g <= MaxDaysNeeded; g++ {
		if tQuantile(float64(2*g-2))*math.Sqrt(spread/float64(g)) < math.Abs(diff) {
			return g, true
		}
	}
	return 0, false
}

func finite(x float64) bool { return !math.IsNaN(x) && !math.IsInf(x, 0) }
