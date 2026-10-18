package metamodel

import "math"

// MinDetectableEdgeBps is the smallest mean net EV per take (bps) the adoption
// bar detects with 80% power (issue #780, decided 2026-10-19: about 11 bps).
// An OOS EV smaller in magnitude cannot be told from zero by a sample of the
// size the bar requires, so it is shown as EVUndetermined instead of as a win
// or a loss. The figure holds under the stated assumptions (sigma, clustering,
// take rate); it is not a promise of any return.
const MinDetectableEdgeBps = 11.0

// EV verdicts (EVVerdict).
const (
	// EVNoData: the model took no OOS signal, so there is no EV to judge. It
	// stays so until the Paper run has produced trades.
	EVNoData = "no_data"
	// EVUndetermined (判定不能): the EV is below MinDetectableEdgeBps in
	// magnitude, or its confidence interval could not be computed, so neither
	// an edge nor its absence can be claimed.
	EVUndetermined = "undetermined"
	// EVPositive: the EV is at least MinDetectableEdgeBps and its interval
	// lies above 0.
	EVPositive = "positive"
	// EVNegative: the EV is at most -MinDetectableEdgeBps and its interval
	// lies below 0.
	EVNegative = "negative"
)

// EVVerdict classifies a model's OOS EV: takes is the number of OOS signals
// behind ev (zero is EVNoData whatever ev is), ev the mean net return per take
// and ci its confidence interval. Only an EV at least MinDetectableEdgeBps in
// magnitude whose interval excludes 0 on its own side is a verdict; everything
// else is EVUndetermined. A bar of samples and a lower limit above 0 are a
// cut-off, not a proof that the model is safe.
func EVVerdict(takes int, ev *float64, ci *Interval) string {
	switch {
	case takes <= 0:
		return EVNoData
	case ev == nil || ci == nil || math.IsNaN(*ev) || math.Abs(*ev) < MinDetectableEdgeBps:
		return EVUndetermined
	case *ev > 0 && ci.Low > 0:
		return EVPositive
	case *ev < 0 && ci.High < 0:
		return EVNegative
	}
	return EVUndetermined
}
