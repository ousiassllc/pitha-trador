package assist

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// OpusReviewPath is the Opus API endpoint the qualitative review request
// is POSTed to.
const OpusReviewPath = "/v1/review"

// Opus API verdict values.
const (
	OpusVerdictApprove = "approve"
	OpusVerdictReject  = "reject"
)

// MaxDrawdownDegradationTolerance is FR-SELFIMPROVE-4's relative
// Max Drawdown degradation ceiling: "既存policy_versionに対し...Max
// Drawdownの悪化が許容範囲内（相対10%以内）". A negative-or-zero
// BaselineMaxDrawdownPct is treated as "no drawdown budget at all" - the
// candidate must match it exactly (0 * 1.10 == 0) - since there is no
// meaningful "10% of zero" to be lenient about.
const MaxDrawdownDegradationTolerance = 0.10

// MinBacktestTrades is the fewest shadow-backtest trades each side (the
// baseline and the candidate pass) must have for the deterministic
// Expectancy/Max Drawdown comparison to count as evidence. Below it the
// comparison is noise - with zero trades both sides are Expectancy 0 /
// Max Drawdown 0, which would trivially satisfy "not worse" - so the
// proposal is rejected as ReasonInsufficientSamples without consulting
// the Opus API.
const MinBacktestTrades = 10

// ReasonInsufficientSamples is the review_json.reason prefix recorded when
// a side of the shadow backtest has fewer than MinBacktestTrades trades.
const ReasonInsufficientSamples = "insufficient_samples"

// backtestMagnitudeEpsilon absorbs float64 arithmetic noise in the
// Max Drawdown comparison so an exact-tolerance candidate is never
// spuriously rejected.
const backtestMagnitudeEpsilon = 1e-9

// BacktestComparison is the shadow-backtest Expectancy/MaxDrawdownPct
// comparison internal/service/selfimprove.Governor computes (reusing
// internal/service/backtest, FR-SELFIMPROVE-4) and passes to Opus for
// review - deliberately plain float64 fields rather than
// backtest.Metrics itself, so this package does not need to know that
// internal package's full shape.
//
// The json tags are the policy_proposals.backtest_result_json storage
// format `GET /api/v1/policy-proposals` reads back.
type BacktestComparison struct {
	BaselineExpectancy      float64 `json:"baseline_expectancy"`
	CandidateExpectancy     float64 `json:"candidate_expectancy"`
	BaselineMaxDrawdownPct  float64 `json:"baseline_max_drawdown_pct"`
	CandidateMaxDrawdownPct float64 `json:"candidate_max_drawdown_pct"`
	// BaselineTradeCount/CandidateTradeCount are the trades behind each
	// side's metrics (0 in rows stored before they were recorded).
	BaselineTradeCount  int `json:"baseline_trade_count"`
	CandidateTradeCount int `json:"candidate_trade_count"`
}

// OpusReviewInput is everything Opus reviews for one proposal: Sol's
// rationale, the (already machine-validated) changes, and the shadow
// backtest comparison.
type OpusReviewInput struct {
	RationaleJSON string
	Changes       []domain.PolicyChange
	Comparison    BacktestComparison
}

// OpusReview is Opus's approve/reject decision, marshaled into
// policy_proposals.review_json. Verdict is "approve" or "reject";
// LLMReviewed is false when the deterministic FR-SELFIMPROVE-4 thresholds
// already failed, in which case the Opus API was not consulted (its answer
// could never turn a threshold miss into an approval, FR-SELFIMPROVE-9).
type OpusReview struct {
	Verdict                 string  `json:"verdict"`
	Approved                bool    `json:"approved"`
	DeterministicPassed     bool    `json:"deterministic_passed"`
	LLMReviewed             bool    `json:"llm_reviewed"`
	ExpectancyNotWorse      bool    `json:"expectancy_not_worse"`
	MaxDrawdownWithinBudget bool    `json:"max_drawdown_within_budget"`
	BaselineExpectancy      float64 `json:"baseline_expectancy"`
	CandidateExpectancy     float64 `json:"candidate_expectancy"`
	BaselineMaxDrawdownPct  float64 `json:"baseline_max_drawdown_pct"`
	CandidateMaxDrawdownPct float64 `json:"candidate_max_drawdown_pct"`
	BaselineTradeCount      int     `json:"baseline_trade_count"`
	CandidateTradeCount     int     `json:"candidate_trade_count"`
	SufficientSamples       bool    `json:"sufficient_samples"`
	Reason                  string  `json:"reason"`
}

// Opus is the Govern adapter. A proposal is approved only when it meets
// FR-SELFIMPROVE-4's deterministic shadow-backtest thresholds AND the
// external Opus LLM API's qualitative review approves it
// (FR-SELFIMPROVE-9). The LLM is an additional veto only.
type Opus struct {
	client *Client
}

// NewOpus returns an Opus adapter that calls client
// (OPUS_API_KEY/OPUS_BASE_URL).
func NewOpus(client *Client) *Opus {
	return &Opus{client: client}
}

type opusRequest struct {
	Task          string            `json:"task"`
	Proposal      opusProposal      `json:"proposal"`
	Backtest      opusBacktest      `json:"backtest"`
	Deterministic opusDeterministic `json:"deterministic"`
}

type opusProposal struct {
	Rationale json.RawMessage `json:"rationale"`
	Changes   []opusChange    `json:"changes"`
}

type opusChange struct {
	Key      string `json:"key"`
	OldValue string `json:"old_value"`
	NewValue string `json:"new_value"`
}

type opusBacktest struct {
	BaselineExpectancy      float64 `json:"baseline_expectancy"`
	CandidateExpectancy     float64 `json:"candidate_expectancy"`
	BaselineMaxDrawdownPct  float64 `json:"baseline_max_drawdown_pct"`
	CandidateMaxDrawdownPct float64 `json:"candidate_max_drawdown_pct"`
	BaselineTradeCount      int     `json:"baseline_trade_count"`
	CandidateTradeCount     int     `json:"candidate_trade_count"`
}

type opusDeterministic struct {
	ExpectancyNotWorse      bool `json:"expectancy_not_worse"`
	MaxDrawdownWithinBudget bool `json:"max_drawdown_within_budget"`
	Passed                  bool `json:"passed"`
}

// OpusResponse is the JSON body the Opus API answers with.
type OpusResponse struct {
	Verdict string `json:"verdict"`
	Reason  string `json:"reason"`
}

// Review implements FR-SELFIMPROVE-4 + FR-SELFIMPROVE-9. It returns the
// approve/reject bool alongside the OpusReview JSON
// internal/service/selfimprove.Governor stores as
// policy_proposals.review_json.
//
// The deterministic rule is evaluated first: a proposal with fewer than
// MinBacktestTrades shadow-backtest trades on either side has no evidence
// to judge and is rejected as ReasonInsufficientSamples; one whose shadow-
// backtest Expectancy is worse than baseline or whose Max Drawdown
// degradation exceeds MaxDrawdownDegradationTolerance (relative) is
// rejected without calling the Opus API. Otherwise the Opus API is asked
// for a qualitative review, and only its "approve" verdict approves the
// proposal. An API failure (including ErrNotConfigured) or an
// unrecognized verdict is an error: the proposal is neither approved nor
// rejected, and the review is retried the next business day
// (overview.md §8).
func (o *Opus) Review(ctx context.Context, in OpusReviewInput) (bool, string, error) {
	cmp := in.Comparison
	expectancyNotWorse := cmp.CandidateExpectancy >= cmp.BaselineExpectancy
	maxDrawdownBudget := cmp.BaselineMaxDrawdownPct * (1 + MaxDrawdownDegradationTolerance)
	maxDrawdownWithinBudget := cmp.CandidateMaxDrawdownPct <= maxDrawdownBudget+backtestMagnitudeEpsilon
	sufficientSamples := cmp.BaselineTradeCount >= MinBacktestTrades && cmp.CandidateTradeCount >= MinBacktestTrades
	deterministicPassed := sufficientSamples && expectancyNotWorse && maxDrawdownWithinBudget

	review := OpusReview{
		DeterministicPassed:     deterministicPassed,
		ExpectancyNotWorse:      expectancyNotWorse,
		MaxDrawdownWithinBudget: maxDrawdownWithinBudget,
		BaselineExpectancy:      cmp.BaselineExpectancy,
		CandidateExpectancy:     cmp.CandidateExpectancy,
		BaselineMaxDrawdownPct:  cmp.BaselineMaxDrawdownPct,
		CandidateMaxDrawdownPct: cmp.CandidateMaxDrawdownPct,
		BaselineTradeCount:      cmp.BaselineTradeCount,
		CandidateTradeCount:     cmp.CandidateTradeCount,
		SufficientSamples:       sufficientSamples,
	}

	if !deterministicPassed {
		review.Verdict = OpusVerdictReject
		review.Reason = deterministicRejectReason(sufficientSamples, cmp, expectancyNotWorse, maxDrawdownWithinBudget)
		return encodeReview(review)
	}

	resp, err := o.callAPI(ctx, in, expectancyNotWorse, maxDrawdownWithinBudget)
	if err != nil {
		return false, "", err
	}
	review.LLMReviewed = true
	review.Verdict = resp.Verdict
	review.Approved = resp.Verdict == OpusVerdictApprove
	review.Reason = resp.Reason
	if review.Reason == "" {
		review.Reason = "opus review returned no reason"
	}
	return encodeReview(review)
}

func (o *Opus) callAPI(ctx context.Context, in OpusReviewInput, expectancyNotWorse, maxDrawdownWithinBudget bool) (OpusResponse, error) {
	changes := make([]opusChange, 0, len(in.Changes))
	for _, c := range in.Changes {
		changes = append(changes, opusChange{Key: c.Key, OldValue: c.OldValue, NewValue: c.NewValue})
	}
	rationale := json.RawMessage(in.RationaleJSON)
	if !json.Valid(rationale) {
		// rationale_json is Sol's own text; never let an odd value break the request.
		quoted, err := json.Marshal(in.RationaleJSON)
		if err != nil {
			return OpusResponse{}, fmt.Errorf("assist: opus: encode rationale: %w", err)
		}
		rationale = quoted
	}

	req := opusRequest{
		Task:     "review_policy_proposal",
		Proposal: opusProposal{Rationale: rationale, Changes: changes},
		Backtest: opusBacktest{
			BaselineExpectancy:      in.Comparison.BaselineExpectancy,
			CandidateExpectancy:     in.Comparison.CandidateExpectancy,
			BaselineMaxDrawdownPct:  in.Comparison.BaselineMaxDrawdownPct,
			CandidateMaxDrawdownPct: in.Comparison.CandidateMaxDrawdownPct,
			BaselineTradeCount:      in.Comparison.BaselineTradeCount,
			CandidateTradeCount:     in.Comparison.CandidateTradeCount,
		},
		Deterministic: opusDeterministic{
			ExpectancyNotWorse:      expectancyNotWorse,
			MaxDrawdownWithinBudget: maxDrawdownWithinBudget,
			Passed:                  true,
		},
	}

	var resp OpusResponse
	if err := o.client.PostJSON(ctx, OpusReviewPath, req, &resp); err != nil {
		return OpusResponse{}, fmt.Errorf("assist: opus review: %w", err)
	}
	if resp.Verdict != OpusVerdictApprove && resp.Verdict != OpusVerdictReject {
		return OpusResponse{}, fmt.Errorf("assist: opus review: unrecognized verdict %q", resp.Verdict)
	}
	return resp, nil
}

func encodeReview(review OpusReview) (bool, string, error) {
	data, err := json.Marshal(review)
	if err != nil {
		return false, "", fmt.Errorf("assist: opus: encode review: %w", err)
	}
	return review.Approved, string(data), nil
}

func deterministicRejectReason(sufficientSamples bool, cmp BacktestComparison, expectancyNotWorse, maxDrawdownWithinBudget bool) string {
	switch {
	case !sufficientSamples:
		return fmt.Sprintf("%s: shadow backtest needs at least %d trades per side, got baseline=%d candidate=%d (FR-SELFIMPROVE-4)",
			ReasonInsufficientSamples, MinBacktestTrades, cmp.BaselineTradeCount, cmp.CandidateTradeCount)
	case !expectancyNotWorse && !maxDrawdownWithinBudget:
		return "shadow backtest Expectancy worsened and Max Drawdown exceeded the 10% relative budget (FR-SELFIMPROVE-4)"
	case !expectancyNotWorse:
		return "shadow backtest Expectancy worsened versus the current policy_version (FR-SELFIMPROVE-4)"
	default:
		return "shadow backtest Max Drawdown exceeded the 10% relative budget versus the current policy_version (FR-SELFIMPROVE-4)"
	}
}
