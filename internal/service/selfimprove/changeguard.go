package selfimprove

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/assist"
	"github.com/ousiassllc/pitha-trador/internal/service/changeguard"
)

// rejectIfChangeBlocked enforces FR-SELFIMPROVE-10 before any backtest or Opus
// call: when the last policy.* change (apply or rollback) is inside the rate
// limit, or a meta-labeling model was adopted inside the freeze, the pending
// proposal is rejected by the Governor with the reason recorded in
// review_json (the same keys as every other Governor rejection) and nothing is
// applied. It reports whether the proposal was rejected.
func (g *Governor) rejectIfChangeBlocked(ctx context.Context, proposalID int64) (bool, error) {
	if g.models == nil {
		return false, nil
	}
	var h changeguard.History
	changedAt, found, err := g.proposals.LastChangeAt(ctx)
	if err != nil {
		return false, fmt.Errorf("selfimprove: last policy change: %w", err)
	}
	if found {
		h.LastPolicyChange = changedAt
	}
	adoptedAt, found, err := g.models.LastAdoptedAt(ctx)
	if err != nil {
		return false, fmt.Errorf("selfimprove: last model adoption: %w", err)
	}
	if found {
		h.LastModelAdoption = adoptedAt
	}
	block := g.limits.CheckPolicyChange(g.now(), h)
	if block == nil {
		return false, nil
	}
	reviewJSON, err := json.Marshal(governorReview{
		Verdict: assist.OpusVerdictReject,
		Reason:  block.Reason,
		Detail:  fmt.Sprintf("%s; allowed from %s", block.Detail, block.Until.Format("2006-01-02")),
	})
	if err != nil {
		return false, fmt.Errorf("selfimprove: encode change-limit review: %w", err)
	}
	if err := g.proposals.UpdateReview(ctx, proposalID, domain.PolicyProposalStatusRejected, reviewedByGovernor, string(reviewJSON)); err != nil {
		return false, fmt.Errorf("selfimprove: record change-limit rejection for proposal %d: %w", proposalID, err)
	}
	slog.WarnContext(ctx, "selfimprove: proposal rejected by the change limit", "proposal_id", proposalID,
		"reason", block.Reason, "detail", block.Detail, "allowed_from", block.Until)
	return true, nil
}
