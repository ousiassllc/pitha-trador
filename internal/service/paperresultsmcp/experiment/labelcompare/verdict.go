package labelcompare

import "math"

// needed is the days-still-needed figure of the primary series (the stored
// label of LONG / SHORT decisions, both sides together). all are the two
// windows' day aggregates, c their comparison.
func needed(all [2]series, c SideCompare) *DaysNeeded {
	observed := min(c.Before.Days, c.After.Days)
	d := &DaysNeeded{
		ObservedPerSide: observed, MinDays: MinDaysFloor, MinDaysSource: MinDaysSource,
		DaysToMin: max(0, MinDaysFloor-observed),
	}
	b, _ := dayMeans(all[0])
	a, _ := dayMeans(all[1])
	varBefore, okB := longRunVariance(b, NeweyWestLagDays)
	varAfter, okA := longRunVariance(a, NeweyWestLagDays)
	switch {
	case !okB || !okA || c.Diff == nil:
		d.Reason = ReasonVarianceUndefined
		return d
	case math.Abs(c.Diff.DiffBps) < NearZeroBps:
		d.Reason = ReasonDiffNearZero
		return d
	}
	g, ok := daysNeeded(varBefore, varAfter, c.Diff.DiffBps)
	if !ok {
		d.Reason = ReasonBeyondHorizon
		return d
	}
	g = max(g, MinDaysFloor)
	remaining := max(0, g-observed)
	d.NeededPerSide, d.RemainingDays = &g, &remaining
	return d
}

// labelVerdict applies the rules in order: the experiment-level withholding
// (aborted, confounded: the same state the trade verdict uses), too few days,
// switch not confirmed, an interval that does not exist, and then the 95%
// interval of after minus before of the daily mean label net bps decides:
// wholly above 0 is improved, wholly below worsened, otherwise inconclusive.
// There is no drawdown criterion: labels carry no path.
func labelVerdict(withheld string, c SideCompare, d *DaysNeeded) string {
	switch {
	case withheld == VerdictAborted || withheld == VerdictConfounded:
		return withheld
	case d.DaysToMin > 0:
		return VerdictInsufficientDays
	case withheld == VerdictSwitchUnconfirmed:
		return withheld
	case c.Diff == nil || c.Diff.CILow == nil || c.Diff.CIHigh == nil:
		return VerdictNotComputable
	case *c.Diff.CILow > 0:
		return VerdictImproved
	case *c.Diff.CIHigh < 0:
		return VerdictWorsened
	}
	return VerdictInconclusive
}

// judged reports whether v is an answer of the interval rather than a
// withheld judgement.
func judged(v string) bool {
	switch v {
	case VerdictImproved, VerdictWorsened, VerdictInconclusive:
		return true
	}
	return false
}
