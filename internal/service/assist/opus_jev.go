package assist

import (
	"context"
	"fmt"

	"github.com/ousiassllc/pitha-trador/internal/service/jev/systemone"
)

const (
	opusQAdopt = "adopt"
	// opusAdoptThreshold is the Jev adoption probability at or above which
	// Opus approves. Below it (or on any failure) the proposal is not
	// approved: Jev can veto a deterministic pass, never rescue a miss.
	opusAdoptThreshold = 0.5
)

// opusQuestions is Opus's single noul question: should this threshold
// proposal be adopted (FR-SELFIMPROVE-9)? Changing the wording changes what
// the recorded probability means.
var opusQuestions = map[string]systemone.Question{
	opusQAdopt: systemone.NoulQuestion(
		"Using `proposal` (the changes to the trading policy thresholds, with their rationale) and `backtest` (shadow-backtest expectancy, maximum drawdown and trade counts "+
			"of the current policy `baseline` versus the proposed `candidate`), should the proposed change be adopted? "+
			"The deterministic checks in `deterministic` have already passed. Judge whether the improvement looks robust rather than a small-sample artifact, "+
			"whether the trade counts are large enough to trust, and whether the change is plausibly safe for live trading.",
		"The improvement is clear and robust enough, the sample is adequate, and the change looks safe to adopt.",
		"The improvement is marginal or likely a small-sample artifact, the evidence is thin, or the change looks risky.",
	),
}

// reviewWithJev asks Jev the adoption question about req and maps the
// noul answer onto the Opus verdict. Jev returns no free text, so the
// reason is built from the probability.
func (o *Opus) reviewWithJev(ctx context.Context, req opusRequest) (OpusResponse, error) {
	result, err := o.jev.Ask(ctx, "opus", req, opusQuestions)
	if err != nil {
		return OpusResponse{}, err
	}
	p := result.Noul(opusQAdopt)
	verdict, outcome := OpusVerdictReject, "below"
	if p >= opusAdoptThreshold {
		verdict, outcome = OpusVerdictApprove, "at or above"
	}
	return OpusResponse{
		Verdict: verdict,
		Reason:  fmt.Sprintf("jev adoption probability %.2f is %s the %.2f threshold (model %s)", p, outcome, opusAdoptThreshold, result.Model),
	}, nil
}
