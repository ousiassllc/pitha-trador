package selfimprove

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/assist"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest"
)

// EvaluateProposal runs FR-SELFIMPROVE-4's shadow backtest for a pending
// proposal, has Opus review the comparison, and - on approval - applies
// it immediately (FR-SELFIMPROVE-5). It returns the approve/reject
// outcome. Approval needs both the deterministic thresholds and the Opus
// API's approval (FR-SELFIMPROVE-9). An Opus API failure returns an
// *aiStageError for stage "opus" and leaves the proposal pending.
func (g *Governor) EvaluateProposal(ctx context.Context, proposalID int64) (bool, error) {
	proposal, err := g.proposals.Get(ctx, proposalID)
	if err != nil {
		return false, fmt.Errorf("selfimprove: get proposal %d: %w", proposalID, err)
	}
	if proposal.Status != domain.PolicyProposalStatusPending {
		return false, fmt.Errorf("selfimprove: proposal %d has status %q, want pending", proposalID, proposal.Status)
	}

	changes, err := domain.ParsePolicyChanges(proposal.ProposedChangesJSON)
	if err != nil {
		return false, fmt.Errorf("selfimprove: parse proposal %d changes: %w", proposalID, err)
	}
	if err := domain.ValidatePolicyChanges(changes); err != nil {
		return false, fmt.Errorf("selfimprove: proposal %d failed validation: %w", proposalID, err)
	}

	current, err := g.CurrentThresholds(ctx)
	if err != nil {
		return false, err
	}
	candidate, err := applyChangesToPolicyConfig(current, changes)
	if err != nil {
		return false, err
	}

	now := g.now()
	period := backtest.Period{Start: businessDaysBefore(now, shadowBacktestLookbackDays), End: now}

	baselineMetrics, err := g.runShadowBacktest(ctx, period, nil)
	if err != nil {
		return false, fmt.Errorf("selfimprove: baseline shadow backtest for proposal %d: %w", proposalID, err)
	}
	candidateMetrics, err := g.runShadowBacktest(ctx, period, &candidate)
	if err != nil {
		return false, fmt.Errorf("selfimprove: candidate shadow backtest for proposal %d: %w", proposalID, err)
	}

	cmp := assist.BacktestComparison{
		BaselineExpectancy:      baselineMetrics.Expectancy,
		CandidateExpectancy:     candidateMetrics.Expectancy,
		BaselineMaxDrawdownPct:  baselineMetrics.MaxDrawdownPct,
		CandidateMaxDrawdownPct: candidateMetrics.MaxDrawdownPct,
	}
	backtestResultJSON, err := json.Marshal(cmp)
	if err != nil {
		return false, fmt.Errorf("selfimprove: encode backtest result for proposal %d: %w", proposalID, err)
	}
	if err := g.proposals.UpdateBacktestResult(ctx, proposalID, string(backtestResultJSON)); err != nil {
		return false, fmt.Errorf("selfimprove: store backtest result for proposal %d: %w", proposalID, err)
	}

	approved, reviewJSON, err := g.opus.Review(ctx, assist.OpusReviewInput{
		RationaleJSON: proposal.RationaleJSON,
		Changes:       changes,
		Comparison:    cmp,
	})
	if err != nil {
		// The proposal stays pending (its backtest result is already
		// stored); RunDaily retries the review the next business day.
		return false, &aiStageError{stage: stageOpus, err: fmt.Errorf("selfimprove: opus review for proposal %d: %w", proposalID, err)}
	}

	if !approved {
		if err := g.proposals.UpdateReview(ctx, proposalID, domain.PolicyProposalStatusRejected, "opus", reviewJSON); err != nil {
			return false, fmt.Errorf("selfimprove: record rejection for proposal %d: %w", proposalID, err)
		}
		return false, nil
	}

	if err := g.proposals.UpdateReview(ctx, proposalID, domain.PolicyProposalStatusApproved, "opus", reviewJSON); err != nil {
		return false, fmt.Errorf("selfimprove: record approval for proposal %d: %w", proposalID, err)
	}
	if err := g.apply(ctx, proposalID, changes, now); err != nil {
		return false, err
	}
	return true, nil
}

// ApplyApproved finishes the apply of a proposal EvaluateProposal approved
// but could not apply (a runtime_settings write or the status update
// failed, leaving status=approved, FR-SELFIMPROVE-5) so it converges to
// applied and enters FR-SELFIMPROVE-6's tracking. Every Set is an upsert,
// so re-running a partly-done apply is safe. RunDaily calls it for every
// approved proposal before anything else.
func (g *Governor) ApplyApproved(ctx context.Context, proposalID int64) error {
	proposal, err := g.proposals.Get(ctx, proposalID)
	if err != nil {
		return fmt.Errorf("selfimprove: get proposal %d: %w", proposalID, err)
	}
	if proposal.Status != domain.PolicyProposalStatusApproved {
		return fmt.Errorf("selfimprove: proposal %d has status %q, want approved", proposalID, proposal.Status)
	}
	changes, err := domain.ParsePolicyChanges(proposal.ProposedChangesJSON)
	if err != nil {
		return fmt.Errorf("selfimprove: parse proposal %d changes: %w", proposalID, err)
	}
	return g.apply(ctx, proposalID, changes, g.now())
}

// apply writes every change's NewValue to runtime_settings and marks
// proposal applied (FR-SELFIMPROVE-5). The Slack notification is
// best-effort: once the proposal is applied a notifier failure is logged,
// never reported as a failed apply (see Notifier).
func (g *Governor) apply(ctx context.Context, proposalID int64, changes []domain.PolicyChange, now time.Time) error {
	for _, c := range changes {
		if err := g.settings.Set(ctx, c.Key, c.NewValue, now); err != nil {
			return fmt.Errorf("selfimprove: apply proposal %d: set %s: %w", proposalID, c.Key, err)
		}
	}
	appliedPolicyVersion := fmt.Sprintf("sol-%d", proposalID)
	if err := g.proposals.MarkApplied(ctx, proposalID, appliedPolicyVersion, now); err != nil {
		return fmt.Errorf("selfimprove: mark proposal %d applied: %w", proposalID, err)
	}
	applied, err := g.proposals.Get(ctx, proposalID)
	if err != nil {
		slog.ErrorContext(ctx, "selfimprove: reload applied proposal for notification failed", "proposal_id", proposalID, "error", err)
		return nil
	}
	if err := g.notifier.ProposalApplied(ctx, applied); err != nil {
		slog.ErrorContext(ctx, "selfimprove: notify proposal applied failed", "proposal_id", proposalID, "error", err)
	}
	return nil
}
