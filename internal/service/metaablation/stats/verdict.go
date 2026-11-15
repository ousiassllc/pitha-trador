package stats

import (
	"fmt"
	"math"
)

// Reasons a comparison is 優劣未確定 (all that apply are listed).
const (
	ReasonUnsetPrefix   = "unset:" // + the unset pre-registered field
	ReasonUndefined     = "estimate_undefined"
	ReasonNoInterval    = "ci_unavailable"
	ReasonCIContains0   = "ci_contains_zero"
	ReasonDaysBelowMin  = "days_below_min_days"
	ReasonCountBelow    = "count_below_min_count"
	ReasonMDEAboveDelta = "detectable_minimum_difference_above_delta"
)

// Counts are the sizes behind a comparison, shown next to every result.
type Counts struct {
	Days               int `json:"days"`
	EligibleCandidates int `json:"eligible_candidates"`
	TakesFrom          int `json:"takes_from"`
	TakesTo            int `json:"takes_to"`
	// EntriesFrom and EntriesTo are the replay layer's entered positions.
	EntriesFrom int `json:"entries_from"`
	EntriesTo   int `json:"entries_to"`
	// Discordant is the number of pairs whose two cells acted differently.
	Discordant int `json:"discordant_pairs"`
	// Basis is the count MinCount is compared with (the label layer's
	// discordant pairs; the replay layer's entries of the smaller cell) and
	// BasisName says what it is.
	Basis     int    `json:"basis"`
	BasisName string `json:"basis_name"`
}

// Result is one comparison To - From with its verdict. Reference comparisons
// (the diagonals) are descriptive only: no interval, no detectable difference,
// Verdict "", and they are not members of the multiple-comparison family.
type Result struct {
	Name      string   `json:"name"`
	Reference bool     `json:"reference"`
	Estimate  Estimate `json:"estimate"`
	Counts    Counts   `json:"counts"`
	// AlphaAdjusted is the per-comparison level after the correction;
	// Correction and NComparisons record how and over how many comparisons.
	AlphaAdjusted float64 `json:"alpha_adjusted"`
	Correction    string  `json:"correction"`
	NComparisons  int     `json:"n_comparisons"`
	// MDE is the detectable minimum difference at the pre-registered power from
	// the observed variance (the bootstrap SE); never a post-hoc power of the
	// observed difference. nil when it cannot be computed.
	MDE     *float64 `json:"detectable_minimum_difference"`
	Verdict Verdict  `json:"verdict"`
	Reasons []string `json:"reasons"`
}

// Decide turns an estimate and its counts into a Result under plan. A
// non-reference result is 優劣未確定 when any pre-registered value is unset,
// the days or count are below their minimum, the interval is missing or holds
// 0, or the detectable minimum difference exceeds delta; otherwise the sign of
// the interval decides.
func Decide(plan Plan, name string, reference bool, est Estimate, counts Counts) Result {
	r := Result{
		Name: name, Reference: reference, Estimate: est, Counts: counts,
		AlphaAdjusted: plan.AlphaAdjusted(), Correction: plan.Correction, NComparisons: plan.NComparisons,
	}
	if reference {
		// Reference comparisons are descriptive: no interval, no detectable
		// difference, no verdict, and they are not counted in the family.
		r.Estimate.CI, r.Estimate.SE = nil, 0
		return r
	}
	if plan.Power != nil && est.CI != nil {
		mde := DetectableMinimum(r.AlphaAdjusted, *plan.Power, est.SE)
		r.MDE = &mde
	}
	for _, f := range plan.UnsetFields() {
		r.Reasons = append(r.Reasons, ReasonUnsetPrefix+f)
	}
	switch {
	case !est.Defined:
		r.Reasons = append(r.Reasons, ReasonUndefined)
	case est.CI == nil:
		r.Reasons = append(r.Reasons, ReasonNoInterval)
	case est.CI.Contains(0):
		r.Reasons = append(r.Reasons, ReasonCIContains0)
	}
	if plan.MinDays != nil && counts.Days < *plan.MinDays {
		r.Reasons = append(r.Reasons, ReasonDaysBelowMin)
	}
	if plan.MinCount != nil && counts.Basis < *plan.MinCount {
		r.Reasons = append(r.Reasons, ReasonCountBelow)
	}
	if plan.Delta != nil && r.MDE != nil && *r.MDE > *plan.Delta {
		r.Reasons = append(r.Reasons, ReasonMDEAboveDelta)
	}
	switch {
	case len(r.Reasons) > 0:
		r.Verdict = Undetermined
	case est.CI.Lower > 0:
		r.Verdict = Superior
	default:
		r.Verdict = Inferior
	}
	return r
}

// DayValue is one trading day's value of a daily series (a daily PnL in yen,
// or a return on capital).
type DayValue struct {
	Day   string
	Value float64
}

// DailyDifference compares two aligned daily series (the same days, taken from
// one continuous replay each): the unit is the day, the statistic the mean of
// the daily differences To - From, the interval a day bootstrap. It is what the
// replay layer reports; path-dependent portfolio results are never compared by
// decision id.
func DailyDifference(plan Plan, name string, reference bool, from, to []DayValue, counts Counts) (Result, error) {
	if len(from) != len(to) {
		return Result{}, fmt.Errorf("stats: %s: series have %d and %d days", name, len(from), len(to))
	}
	clusters := make([]Cluster, len(from))
	for i := range from {
		if from[i].Day != to[i].Day {
			return Result{}, fmt.Errorf("stats: %s: day %d is %q in one series and %q in the other", name, i, from[i].Day, to[i].Day)
		}
		if math.IsNaN(from[i].Value) || math.IsNaN(to[i].Value) {
			return Result{}, fmt.Errorf("stats: %s: NaN on %s", name, from[i].Day)
		}
		clusters[i] = Cluster{Sum: to[i].Value - from[i].Value, N: 1}
	}
	counts.Days = len(clusters)
	if reference {
		return Decide(plan, name, true, Describe(clusters), counts), nil
	}
	return Decide(plan, name, false, BootstrapFor(plan, name, clusters), counts), nil
}

// BootstrapFor is Bootstrap at the plan's resamples, adjusted alpha and a seed
// derived from the plan's seed and the comparison name.
func BootstrapFor(plan Plan, name string, clusters []Cluster) Estimate {
	return Bootstrap(clusters, plan.Resamples, plan.AlphaAdjusted(), seedFor(plan.Seed, name))
}

// CheckFamily reports an error when more comparisons were run than the plan
// pre-registered: the actual total, exploratory comparisons included, must be
// within NComparisons for the correction to be valid.
func CheckFamily(plan Plan, executed int) error {
	if executed > plan.NComparisons {
		return fmt.Errorf("stats: %d comparisons were run but %d are pre-registered; start a new run with the actual count", executed, plan.NComparisons)
	}
	return nil
}
