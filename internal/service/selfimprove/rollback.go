package selfimprove

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// TrackAndRollback checks a single applied proposal's post-apply
// realized Expectancy (FR-SELFIMPROVE-6). Before the tracking window
// (postApplyTrackingDays business days after AppliedAt) has closed, or
// for a proposal that is not currently status=applied, it is a no-op
// returning (false, nil). Once the window has closed, it compares the
// mean realized_pnl of positions closed in the postApplyTrackingDays
// window before/after AppliedAt; a >=20% relative degradation reverts
// every change to its OldValue and marks the proposal rolled_back.
// If either window has no closed position the comparison is
// indeterminate and nothing is rolled back; the proposal stays
// status=applied and is re-evaluated on every later run (the windows are
// anchored on AppliedAt, so there is no timeout/cut-off).
func (g *Governor) TrackAndRollback(ctx context.Context, proposalID int64) (bool, error) {
	proposal, err := g.proposals.Get(ctx, proposalID)
	if err != nil {
		return false, fmt.Errorf("selfimprove: get proposal %d: %w", proposalID, err)
	}
	if proposal.Status != domain.PolicyProposalStatusApplied || proposal.AppliedAt == nil {
		return false, nil
	}

	appliedAt := *proposal.AppliedAt
	trackingEnd := businessDaysAfter(appliedAt, postApplyTrackingDays)
	now := g.now()
	if now.Before(trackingEnd) {
		return false, nil
	}

	preStart := businessDaysBefore(appliedAt, postApplyTrackingDays)
	preExpectancy, preOK, err := g.realizedExpectancy(ctx, preStart, appliedAt)
	if err != nil {
		return false, fmt.Errorf("selfimprove: pre-apply realized expectancy for proposal %d: %w", proposalID, err)
	}
	postExpectancy, postOK, err := g.realizedExpectancy(ctx, appliedAt, trackingEnd)
	if err != nil {
		return false, fmt.Errorf("selfimprove: post-apply realized expectancy for proposal %d: %w", proposalID, err)
	}
	if !preOK || !postOK {
		return false, nil
	}

	if !expectancyDegraded(preExpectancy, postExpectancy) {
		return false, nil
	}

	changes, err := domain.ParsePolicyChanges(proposal.ProposedChangesJSON)
	if err != nil {
		return false, fmt.Errorf("selfimprove: parse proposal %d changes: %w", proposalID, err)
	}
	if err := domain.ValidatePolicyChanges(changes); err != nil {
		// Defense in depth (same rationale as EvaluateProposal/
		// ProposeDaily): never write a runtime_settings key that is not
		// a validated policy.* threshold, even while reverting.
		return false, fmt.Errorf("selfimprove: proposal %d failed validation, refusing to roll back: %w", proposalID, err)
	}
	for _, c := range changes {
		if err := g.settings.Set(ctx, c.Key, c.OldValue, now); err != nil {
			return false, fmt.Errorf("selfimprove: rollback proposal %d: restore %s: %w", proposalID, c.Key, err)
		}
	}

	reason := fmt.Sprintf(
		"realized expectancy degraded from %.4f to %.4f (>=%.0f%% relative) over the %d-business-day post-apply tracking window (FR-SELFIMPROVE-6)",
		preExpectancy, postExpectancy, expectancyDegradationTolerance*100, postApplyTrackingDays,
	)
	if err := g.proposals.MarkRolledBack(ctx, proposalID, now, reason); err != nil {
		return false, fmt.Errorf("selfimprove: mark proposal %d rolled back: %w", proposalID, err)
	}
	rolledBack, err := g.proposals.Get(ctx, proposalID)
	if err != nil {
		return false, fmt.Errorf("selfimprove: reload rolled-back proposal %d: %w", proposalID, err)
	}
	if err := g.notifier.ProposalRolledBack(ctx, rolledBack, reason); err != nil {
		return false, fmt.Errorf("selfimprove: notify proposal %d rolled back: %w", proposalID, err)
	}
	return true, nil
}

// expectancyDegraded reports whether post is a worsening of pre of at
// least expectancyDegradationTolerance relative to |pre|
// (FR-SELFIMPROVE-6's "相対20%以上悪化"). The magnitude |pre| is used so
// the test is meaningful for a negative baseline too, and post >= pre
// (unchanged or improved) never counts as degraded. pre == 0 has no
// relative scale, so any post < 0 counts.
func expectancyDegraded(pre, post float64) bool {
	if post >= pre {
		return false
	}
	return pre-post >= math.Abs(pre)*expectancyDegradationTolerance
}

// realizedExpectancy is the mean domain.Position.RealizedPnL of
// positions closed in [start, end). ok is false when no position closed
// in the window: "no data" is distinct from an Expectancy of 0.
func (g *Governor) realizedExpectancy(ctx context.Context, start, end time.Time) (expectancy float64, ok bool, err error) {
	closed, err := g.positions.ListClosedBetween(ctx, start, end)
	if err != nil {
		return 0, false, fmt.Errorf("selfimprove: list closed positions [%s, %s): %w", start, end, err)
	}
	if len(closed) == 0 {
		return 0, false, nil
	}
	var sum float64
	for _, p := range closed {
		if p.RealizedPnL != nil {
			sum += *p.RealizedPnL
		}
	}
	return sum / float64(len(closed)), true, nil
}
