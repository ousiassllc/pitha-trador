package metamodel

import "math"

// MinDetectableEdgeBps is the smallest mean net EV per take (bps) the adoption
// bar detects with 80% power (issue #780, decided 2026-10-19: about 11 bps;
// training's TestAdoptionPower_AtTheMinimumDetectableEdge measures 81.8% at it
// under sigma 30 bps, a day-level shift of sd 12% and the minimum sample). An
// EV below it whose interval still straddles 0 cannot be told from zero by a
// sample of that size, so it is EVUndetermined. The figure holds under those
// assumptions; it is not a promise of any return.
const MinDetectableEdgeBps = 11.0

// EV verdicts (EVVerdict).
const (
	// EVNoData: the model took no OOS signal, so there is no EV to judge. It
	// stays so until there is something to judge, never read as a result.
	EVNoData = "no_data"
	// EVPositive: the interval lies above 0 (the lower limit is above 0).
	EVPositive = "positive"
	// EVNegative: the interval lies below 0 (the upper limit is below 0).
	EVNegative = "negative"
	// EVUndetermined (判定不能): the interval straddles 0 (or is undefined) and
	// the EV is below MinDetectableEdgeBps in magnitude: an edge that small is
	// beyond what the sample can detect, so neither an edge nor its absence
	// can be claimed.
	EVUndetermined = "undetermined"
	// EVNotSignificant: the interval straddles 0 although the EV is at least
	// MinDetectableEdgeBps in magnitude, so the sample was big enough to see
	// such an edge and did not find it: the plain "not adopted" case.
	EVNotSignificant = "not_significant"
)

// EVVerdict classifies a model's OOS EV: takes is the number of OOS signals
// behind ev (zero is EVNoData whatever else is given), ev the mean net return
// per take and ci its confidence interval. Order: no takes, then a lower limit
// above 0 or an upper limit below 0 (a limit exactly at 0 does not exclude it),
// then the size of the EV. Positive and negative are a cut-off passed, not a
// proof that the model is safe.
func EVVerdict(takes int, ev *float64, ci *Interval) string {
	switch {
	case takes <= 0:
		return EVNoData
	case ci != nil && ci.Low > 0:
		return EVPositive
	case ci != nil && ci.High < 0:
		return EVNegative
	case ev == nil || math.IsNaN(*ev) || math.Abs(*ev) < MinDetectableEdgeBps:
		return EVUndetermined
	}
	return EVNotSignificant
}
