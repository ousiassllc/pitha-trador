package labelcompare

import (
	"math"

	"gonum.org/v1/gonum/stat/distuv"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/experiment"
)

// IntervalMethod names how every interval of the label-based side is made
// (Result.IntervalMethod): the day is the unit, and the variance of the mean of
// the day means is the Newey-West (Bartlett kernel) long-run variance, so a
// correlation between neighbouring days (labels of overlapping holds) widens the
// interval instead of being ignored.
const IntervalMethod = "newey_west_bartlett"

// NeweyWestLagDays is the lag of that correction, in days: the longest holding
// time of a label rounded up to whole days. A stored label (calibration_outcomes,
// triple barrier) holds at most barriersettings.MaxTimeoutMinutes = 60 minutes
// and a research label (#1074) at most until the close of the same session
// ("30m" / "60m" / "close"), so no hold reaches past the day it starts on and
// the lag is 1 day. A test in internal/service/paperresultsmcp/experimentlabeltests
// pins it against the barrier timeout (this package may not import config).
const NeweyWestLagDays = 1

// longRunVariance is the variance of one observation of xs that makes
// sqrt(v/n) the standard error of the mean when neighbouring observations are
// correlated up to lag: the Newey-West estimator, Bartlett weights 1 - l/(lag+1),
//
//	v = n/(n-1) * (g0 + 2 * sum_{l=1..lag} (1 - l/(lag+1)) * g_l),
//	g_l = (1/n) * sum_{t=l..n-1} (x_t - m)(x_{t-l} - m).
//
// xs are the day means in day order, and "neighbouring" is the next day that
// has an observation (a day without a labeled decision is not a step: a weekend
// is not a gap, and a skipped day counts as adjacent, which is the cautious
// side). With lag 0 it is the sample variance (n-1), the variance the interval
// used before the correction. The Bartlett kernel keeps v >= 0. ok is false with
// fewer than two values.
//
// With few days the estimate is biased low (it estimates lag autocovariances
// from n-lag pairs), so it narrows less than the true correlation warrants; the
// interval stays a guide, as docs/architecture/paper-results-mcp-experiments-labels.md §2.2 says.
func longRunVariance(xs []float64, lag int) (v float64, ok bool) {
	n := len(xs)
	if n < 2 {
		return 0, false
	}
	lag = min(max(lag, 0), n-1)
	var sum float64
	for _, x := range xs {
		sum += x
	}
	m := sum / float64(n)
	autocov := func(l int) float64 {
		var s float64
		for t := l; t < n; t++ {
			s += (xs[t] - m) * (xs[t-l] - m)
		}
		return s / float64(n)
	}
	v = autocov(0)
	for l := 1; l <= lag; l++ {
		v += 2 * (1 - float64(l)/float64(lag+1)) * autocov(l)
	}
	v *= float64(n) / float64(n-1)
	return max(v, 0), true
}

func meanOf(xs []float64) float64 {
	var s float64
	for _, x := range xs {
		s += x
	}
	return s / float64(len(xs))
}

// meanInterval is the 95% interval of the mean of the day means xs: mean ±
// t(n-1) * sqrt(v/n) with v the long-run variance at lag (NeweyWestLagDays in
// production; 0 is the plain day-cluster interval). The bounds are nil with
// fewer than two days or a non-finite result.
func meanInterval(xs []float64, lag int) (lo, hi *float64) {
	v, ok := longRunVariance(xs, lag)
	if !ok {
		return nil, nil
	}
	n := float64(len(xs))
	half := tQuantile(n-1) * math.Sqrt(v/n)
	m := meanOf(xs)
	l, h := m-half, m+half
	if !finite(half) || !finite(l) || !finite(h) {
		return nil, nil
	}
	return &l, &h
}

// diffOf is after minus before of the two windows' day means with Welch's
// interval, where each window's variance of the mean is its long-run variance
// over its days (v/n) instead of the sample variance over its days: the
// windows are separate periods, so independent of each other, but the days
// inside one are not. The difference is returned whenever both windows have a
// day; the interval only with two days on each side and a non-zero spread (the
// degrees of freedom are Welch-Satterthwaite on those variances). nil without a
// day on a side. lag is NeweyWestLagDays in production.
func diffOf(before, after []float64, lag int) *Diff {
	if len(before) == 0 || len(after) == 0 {
		return nil
	}
	d := meanOf(after) - meanOf(before)
	if !finite(d) {
		return nil
	}
	out := &Diff{DiffBps: d}
	vb, okB := longRunVariance(before, lag)
	va, okA := longRunVariance(after, lag)
	if !okB || !okA {
		return out
	}
	nb, na := float64(len(before)), float64(len(after))
	a, b := vb/nb, va/na // the two parts of the squared standard error
	se2 := a + b
	if !(se2 > 0) || math.IsInf(se2, 0) {
		return out
	}
	se := math.Sqrt(se2)
	df := se2 * se2 / (a*a/(nb-1) + b*b/(na-1))
	if !(df > 0) || math.IsInf(df, 0) {
		return out
	}
	half := tQuantile(df) * se
	lo, hi, t := d-half, d+half, d/se
	if !finite(half) || !finite(lo) || !finite(hi) || !finite(t) {
		return out
	}
	out.CILow, out.CIHigh, out.T, out.DF = &lo, &hi, &t, &df
	return out
}

// tQuantile is the two-sided 95% Student's t quantile at df degrees of freedom
// (non-integer allowed).
func tQuantile(df float64) float64 {
	return distuv.StudentsT{Mu: 0, Sigma: 1, Nu: df}.Quantile(1 - (1-experiment.CILevel)/2)
}
