package selfimprove

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// revertChanges restores the policy.* keys proposal changed
// (FR-SELFIMPROVE-6) and returns the keys it deliberately left alone.
//
// A key is only restored while runtime_settings still holds the value this
// proposal applied (its NewValue). Any other value means a later proposal
// (or a manual edit) has since replaced it, so writing OldValue back would
// silently discard that newer value; such keys are skipped and reported.
//
// The value restored is the proposal's OldValue, except that
// OldValue is followed back through a directly preceding proposal on the
// same key that has itself been rolled back and whose NewValue it is:
// that value was never meant to stay in effect, so reverting to it would
// resurrect a rolled-back proposal.
func (g *Governor) revertChanges(ctx context.Context, proposal domain.PolicyProposal, changes []domain.PolicyChange, now time.Time) (skipped []string, err error) {
	history, err := g.appliedHistory(ctx)
	if err != nil {
		return nil, err
	}
	for _, c := range changes {
		current, ok, err := g.settings.Get(ctx, c.Key)
		if err != nil {
			return nil, fmt.Errorf("selfimprove: rollback proposal %d: read %s: %w", proposal.ID, c.Key, err)
		}
		if !ok || !jsonValueEqual(current, c.NewValue) {
			skipped = append(skipped, c.Key)
			continue
		}
		restore := restoreValue(proposal, c, history)
		if err := g.settings.Set(ctx, c.Key, restore, now); err != nil {
			return nil, fmt.Errorf("selfimprove: rollback proposal %d: restore %s: %w", proposal.ID, c.Key, err)
		}
	}
	return skipped, nil
}

// appliedHistory returns every proposal that was ever applied (status
// applied or rolled_back).
func (g *Governor) appliedHistory(ctx context.Context) ([]domain.PolicyProposal, error) {
	var history []domain.PolicyProposal
	for _, status := range []string{domain.PolicyProposalStatusApplied, domain.PolicyProposalStatusRolledBack} {
		list, err := g.proposals.ListByStatus(ctx, status)
		if err != nil {
			return nil, fmt.Errorf("selfimprove: list %s proposals: %w", status, err)
		}
		history = append(history, list...)
	}
	return history, nil
}

// restoreValue returns the value to write back for change c of proposal:
// c.OldValue, stepping over each directly preceding rolled-back proposal
// on the same key whose NewValue c.OldValue is (see revertChanges).
func restoreValue(proposal domain.PolicyProposal, c domain.PolicyChange, history []domain.PolicyProposal) string {
	value, before := c.OldValue, proposal.AppliedAt
	for before != nil {
		pred, predChange, ok := precedingChange(history, c.Key, *before, proposal.ID)
		if !ok || pred.Status != domain.PolicyProposalStatusRolledBack || !jsonValueEqual(predChange.NewValue, value) {
			break
		}
		value, before = predChange.OldValue, pred.AppliedAt
	}
	return value
}

// precedingChange finds the latest-applied proposal (other than skipID)
// applied strictly before `before` that changed key.
func precedingChange(history []domain.PolicyProposal, key string, before time.Time, skipID int64) (domain.PolicyProposal, domain.PolicyChange, bool) {
	var (
		best       domain.PolicyProposal
		bestChange domain.PolicyChange
		found      bool
	)
	for _, p := range history {
		if p.ID == skipID || p.AppliedAt == nil || !p.AppliedAt.Before(before) {
			continue
		}
		if found && !p.AppliedAt.After(*best.AppliedAt) {
			continue
		}
		changes, err := domain.ParsePolicyChanges(p.ProposedChangesJSON)
		if err != nil {
			continue
		}
		for _, pc := range changes {
			if pc.Key == key {
				best, bestChange, found = p, pc, true
				break
			}
		}
	}
	return best, bestChange, found
}

// jsonValueEqual compares two JSON-encoded scalars by value (so 0.65 and
// 0.650 match), falling back to string equality for undecodable input.
func jsonValueEqual(a, b string) bool {
	var x, y any
	if json.Unmarshal([]byte(a), &x) != nil || json.Unmarshal([]byte(b), &y) != nil {
		return a == b
	}
	return reflect.DeepEqual(x, y)
}
