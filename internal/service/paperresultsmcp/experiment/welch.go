package experiment

import (
	"math"

	"gonum.org/v1/gonum/stat/distuv"
)

// CILevel is the confidence level of the interval experiment_compare returns.
const CILevel = 0.95

// Sample is one period's net pnl per closed trade (JPY, after trading costs):
// the count, the mean and the sample standard deviation (n - 1 in the
// denominator). A figure that is undefined is nil: the mean with no trade, the
// standard deviation with fewer than two.
type Sample struct {
	N       int      `json:"n"`
	MeanJPY *float64 `json:"mean_jpy"`
	SDJPY   *float64 `json:"sd_jpy"`
}

// Test is Welch's two-sample t test of the mean net pnl per trade, after
// minus before, with the unequal-variance (Welch–Satterthwaite) degrees of
// freedom. T, DF, CILow and CIHigh are nil when the test cannot be computed:
// a period has fewer than two trades, or both have a sample variance of 0 (the
// standard error is then 0 and t is undefined). The samples are still given.
type Test struct {
	Before Sample `json:"before"`
	After  Sample `json:"after"`
	// Diff is the difference of the means, after minus before (JPY per trade);
	// nil when a period has no trade.
	Diff *float64 `json:"diff"`
	// CILevel is the confidence level of CILow..CIHigh (always 0.95).
	CILevel float64 `json:"ci_level"`
	// CILow and CIHigh bound the difference of the means, after minus before.
	CILow  *float64 `json:"ci_low"`
	CIHigh *float64 `json:"ci_high"`
	T      *float64 `json:"t"`
	DF     *float64 `json:"df"`
}

// Computable reports whether the interval exists.
func (t Test) Computable() bool { return t.CILow != nil && t.CIHigh != nil }

// Welch tests the mean of after against the mean of before. The series are
// the per-trade net pnl of the two periods (query.PerformanceSummary). It
// never panics or returns a NaN / infinity: whatever cannot be computed is nil.
//
// The Student's t quantile comes from gonum (distuv.StudentsT), which accepts
// the non-integer degrees of freedom Welch–Satterthwaite gives.
func Welch(before, after []float64) Test {
	t := Test{Before: sample(before), After: sample(after), CILevel: CILevel}
	if t.Before.MeanJPY != nil && t.After.MeanJPY != nil {
		if d := *t.After.MeanJPY - *t.Before.MeanJPY; finite(d) {
			t.Diff = &d
		}
	}
	if t.Before.N < 2 || t.After.N < 2 || t.Diff == nil || t.Before.SDJPY == nil || t.After.SDJPY == nil {
		return t
	}
	n1, n2 := float64(t.Before.N), float64(t.After.N)
	sd1, sd2 := *t.Before.SDJPY, *t.After.SDJPY
	a, b := sd1*sd1/n1, sd2*sd2/n2 // the two parts of the squared standard error
	se2 := a + b
	diff := *t.Diff
	if !(se2 > 0) || math.IsInf(se2, 0) {
		return t
	}
	se := math.Sqrt(se2)
	df := se2 * se2 / (a*a/(n1-1) + b*b/(n2-1))
	if !(df > 0) || math.IsInf(df, 0) {
		return t
	}
	half := distuv.StudentsT{Mu: 0, Sigma: 1, Nu: df}.Quantile(1 - (1-CILevel)/2)
	low, high, tv := diff-half*se, diff+half*se, diff/se
	if !finite(half) || !finite(low) || !finite(high) || !finite(tv) {
		return t
	}
	t.CILow, t.CIHigh, t.T, t.DF = &low, &high, &tv, &df
	return t
}

// sample's mean is sum / n in series order, the arithmetic of
// query.Performance.ExpectancyJPY, so the two always agree to the last digit.
func sample(xs []float64) Sample {
	s := Sample{N: len(xs)}
	if len(xs) == 0 {
		return s
	}
	var sum float64
	for _, x := range xs {
		sum += x
	}
	mean := sum / float64(len(xs))
	if !finite(mean) {
		return s
	}
	s.MeanJPY = &mean
	if len(xs) < 2 {
		return s
	}
	var ss float64 // two-pass: robust against cancellation
	for _, x := range xs {
		ss += (x - mean) * (x - mean)
	}
	if sd := math.Sqrt(ss / float64(len(xs)-1)); finite(sd) {
		s.SDJPY = &sd
	}
	return s
}

func finite(x float64) bool { return !math.IsNaN(x) && !math.IsInf(x, 0) }
