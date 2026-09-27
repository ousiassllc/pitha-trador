package assist

import (
	"encoding/json"
	"fmt"
)

// MaxDrawdownDegradationTolerance is FR-SELFIMPROVE-4's relative
// Max Drawdown degradation ceiling: "既存policy_versionに対し...Max
// Drawdownの悪化が許容範囲内（相対10%以内）". A negative-or-zero
// BaselineMaxDrawdownPct is treated as "no drawdown budget at all" - the
// candidate must match it exactly (0 * 1.10 == 0) - since there is no
// meaningful "10% of zero" to be lenient about.
const MaxDrawdownDegradationTolerance = 0.10

// backtestMagnitudeEpsilon absorbs float64 arithmetic noise in the
// Max Drawdown comparison so an exact-tolerance candidate is never
// spuriously rejected.
const backtestMagnitudeEpsilon = 1e-9

// BacktestComparison is the shadow-backtest Expectancy/MaxDrawdownPct
// comparison internal/service/selfimprove.Governor computes (reusing
// internal/service/backtest, FR-SELFIMPROVE-4) and passes to Opus for
// review - deliberately plain float64 fields rather than
// backtest.Metrics itself, so this package (a future external Sol/Opus
// API's request/response shape) does not need to know that internal
// package's full shape.
type BacktestComparison struct {
	BaselineExpectancy      float64
	CandidateExpectancy     float64
	BaselineMaxDrawdownPct  float64
	CandidateMaxDrawdownPct float64
}

// OpusReview is Opus's approve/reject decision, marshaled into
// policy_proposals.review_json.
type OpusReview struct {
	Approved                bool    `json:"approved"`
	ExpectancyNotWorse      bool    `json:"expectancy_not_worse"`
	MaxDrawdownWithinBudget bool    `json:"max_drawdown_within_budget"`
	BaselineExpectancy      float64 `json:"baseline_expectancy"`
	CandidateExpectancy     float64 `json:"candidate_expectancy"`
	BaselineMaxDrawdownPct  float64 `json:"baseline_max_drawdown_pct"`
	CandidateMaxDrawdownPct float64 `json:"candidate_max_drawdown_pct"`
	Reason                  string  `json:"reason"`
}

// Opus is the Govern adapter: it applies FR-SELFIMPROVE-4's exact
// approval rule to a shadow-backtest BacktestComparison.
type Opus struct{}

// NewOpus returns an Opus adapter.
func NewOpus() *Opus {
	return &Opus{}
}

// Review implements FR-SELFIMPROVE-4: a proposal is approved only if its
// shadow-backtest Expectancy is not worse than baseline and its
// Max Drawdown degradation stays within MaxDrawdownDegradationTolerance
// (relative). It returns the approve/reject bool alongside the
// OpusReview JSON internal/service/selfimprove.Governor stores as
// policy_proposals.review_json.
func (o *Opus) Review(cmp BacktestComparison) (bool, string, error) {
	expectancyNotWorse := cmp.CandidateExpectancy >= cmp.BaselineExpectancy
	maxDrawdownBudget := cmp.BaselineMaxDrawdownPct * (1 + MaxDrawdownDegradationTolerance)
	maxDrawdownWithinBudget := cmp.CandidateMaxDrawdownPct <= maxDrawdownBudget+backtestMagnitudeEpsilon

	approved := expectancyNotWorse && maxDrawdownWithinBudget
	review := OpusReview{
		Approved:                approved,
		ExpectancyNotWorse:      expectancyNotWorse,
		MaxDrawdownWithinBudget: maxDrawdownWithinBudget,
		BaselineExpectancy:      cmp.BaselineExpectancy,
		CandidateExpectancy:     cmp.CandidateExpectancy,
		BaselineMaxDrawdownPct:  cmp.BaselineMaxDrawdownPct,
		CandidateMaxDrawdownPct: cmp.CandidateMaxDrawdownPct,
		Reason:                  reviewReason(approved, expectancyNotWorse, maxDrawdownWithinBudget),
	}

	data, err := json.Marshal(review)
	if err != nil {
		return false, "", fmt.Errorf("assist: opus: encode review: %w", err)
	}
	return approved, string(data), nil
}

func reviewReason(approved, expectancyNotWorse, maxDrawdownWithinBudget bool) string {
	if approved {
		return "shadow backtest Expectancy did not worsen and Max Drawdown stayed within the 10% relative budget (FR-SELFIMPROVE-4)"
	}
	switch {
	case !expectancyNotWorse && !maxDrawdownWithinBudget:
		return "shadow backtest Expectancy worsened and Max Drawdown exceeded the 10% relative budget (FR-SELFIMPROVE-4)"
	case !expectancyNotWorse:
		return "shadow backtest Expectancy worsened versus the current policy_version (FR-SELFIMPROVE-4)"
	default:
		return "shadow backtest Max Drawdown exceeded the 10% relative budget versus the current policy_version (FR-SELFIMPROVE-4)"
	}
}
