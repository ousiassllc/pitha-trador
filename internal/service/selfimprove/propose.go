package selfimprove

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/assist"
)

// ReasonLLMOutputOutOfBounds is the review_json.reason recorded when Sol's
// LLM output violates FR-SELFIMPROVE-2/3 (FR-SELFIMPROVE-8).
const ReasonLLMOutputOutOfBounds = "llm_output_out_of_bounds"

// reviewedByGovernor is policy_proposals.reviewed_by for a proposal the
// Governor itself rejected before any Opus review.
const reviewedByGovernor = "governor"

// governorReview is the review_json of a proposal the Governor rejected
// itself (same verdict/approved/reason keys as assist.OpusReview so the
// audit API reads both alike).
type governorReview struct {
	Verdict  string `json:"verdict"`
	Approved bool   `json:"approved"`
	Reason   string `json:"reason"`
	Detail   string `json:"detail"`
}

// ProposeDaily runs Sol's daily analysis (FR-SELFIMPROVE-1) and records
// the LLM's proposal as a policy_proposals row. It returns
// (proposal, true, nil) when a row was recorded and (zero, false, nil) when
// Sol proposes nothing today.
//
// Sol's output is untrusted (FR-SELFIMPROVE-8): every change is machine-
// checked against FR-SELFIMPROVE-2 (policy.* keys only) and
// FR-SELFIMPROVE-3 (change width caps), with old_value taken from the
// thresholds actually in effect rather than from anything the LLM claims.
// A violating proposal is recorded with status=rejected and
// review_json.reason=llm_output_out_of_bounds and is never evaluated; a
// conforming one is recorded pending. Callers must check Status.
//
// A Sol API failure returns an *aiStageError for stage "sol".
func (g *Governor) ProposeDaily(ctx context.Context, longCalibration, shortCalibration domain.CalibrationMetrics) (domain.PolicyProposal, bool, error) {
	current, err := g.CurrentThresholds(ctx)
	if err != nil {
		return domain.PolicyProposal{}, false, err
	}

	proposal, ok, err := g.sol.Analyze(ctx, assist.SolAnalysisInput{
		Long:  assist.DirectionCalibration{Thresholds: current.Long, Calibration: longCalibration},
		Short: assist.DirectionCalibration{Thresholds: current.Short, Calibration: shortCalibration},
	})
	if err != nil {
		return domain.PolicyProposal{}, false, &aiStageError{stage: stageSol, err: err}
	}
	if !ok {
		return domain.PolicyProposal{}, false, nil
	}

	changes := policyChangesFromSol(current, proposal.Changes)
	changesJSON, err := domain.EncodePolicyChanges(changes)
	if err != nil {
		return domain.PolicyProposal{}, false, fmt.Errorf("selfimprove: encode sol proposal: %w", err)
	}

	violation := domain.ValidatePolicyChanges(changes)
	status := domain.PolicyProposalStatusPending
	if violation != nil {
		status = domain.PolicyProposalStatusRejected
	}
	stored, err := g.proposals.Insert(ctx, domain.PolicyProposal{
		ProposedAt:          g.now(),
		RationaleJSON:       proposal.RationaleJSON,
		ProposedChangesJSON: changesJSON,
		Status:              status,
	})
	if err != nil {
		return domain.PolicyProposal{}, false, fmt.Errorf("selfimprove: insert proposal: %w", err)
	}
	if violation == nil {
		return stored, true, nil
	}

	reviewJSON, err := json.Marshal(governorReview{
		Verdict: assist.OpusVerdictReject,
		Reason:  ReasonLLMOutputOutOfBounds,
		Detail:  violation.Error(),
	})
	if err != nil {
		return domain.PolicyProposal{}, false, fmt.Errorf("selfimprove: encode rejection review: %w", err)
	}
	if err := g.proposals.UpdateReview(ctx, stored.ID, domain.PolicyProposalStatusRejected, reviewedByGovernor, string(reviewJSON)); err != nil {
		return domain.PolicyProposal{}, false, fmt.Errorf("selfimprove: record out-of-bounds rejection for proposal %d: %w", stored.ID, err)
	}
	rejected, err := g.proposals.Get(ctx, stored.ID)
	if err != nil {
		return domain.PolicyProposal{}, false, fmt.Errorf("selfimprove: reload rejected proposal %d: %w", stored.ID, err)
	}
	return rejected, true, nil
}

// policyChangesFromSol turns Sol's raw changes into domain.PolicyChanges.
// OldValue is the currently-effective value for the key ("null" for a key
// that is not a policy.* threshold at all - ValidatePolicyChanges rejects
// it by key anyway). NewValue is kept as the LLM wrote it (compacted).
func policyChangesFromSol(current config.PolicyConfig, raw []assist.SolChange) []domain.PolicyChange {
	changes := make([]domain.PolicyChange, 0, len(raw))
	for _, c := range raw {
		oldValue, ok := policyFieldJSON(current, c.Key)
		if !ok {
			oldValue = "null"
		}
		newValue := "null"
		var compact bytes.Buffer
		if err := json.Compact(&compact, c.NewValue); err == nil && compact.Len() > 0 {
			newValue = compact.String()
		}
		changes = append(changes, domain.PolicyChange{Key: c.Key, OldValue: oldValue, NewValue: newValue})
	}
	return changes
}
