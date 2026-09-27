package selfimprove_test

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/selfimprove"
)

func TestGovernor_TrackAndRollback_RevertsOnDegradedRealizedExpectancy(t *testing.T) {
	f := newGovernorFixtures(t)
	ctx := context.Background()
	appliedAt := time.Date(2026, 3, 2, 16, 0, 0, 0, time.UTC)
	trackingClosed := appliedAt.Add(10 * 24 * time.Hour) // safely past the ~7-calendar-day (5 business day) window

	f.closePosition(t, appliedAt.Add(-3*24*time.Hour), 1000.0) // pre-apply window
	f.closePosition(t, appliedAt.Add(3*24*time.Hour), 700.0)   // post-apply window: -30% relative

	changesJSON, err := domain.EncodePolicyChanges([]domain.PolicyChange{
		{Key: domain.PolicyKeyLongMinProbability, OldValue: `0.60`, NewValue: `0.65`},
	})
	if err != nil {
		t.Fatalf("EncodePolicyChanges: %v", err)
	}
	if err := f.settings.Set(ctx, domain.PolicyKeyLongMinProbability, `0.65`, appliedAt); err != nil {
		t.Fatalf("seed applied setting: %v", err)
	}
	proposal, err := f.proposals.Insert(ctx, domain.PolicyProposal{RationaleJSON: `{}`, ProposedChangesJSON: changesJSON})
	if err != nil {
		t.Fatalf("Insert proposal: %v", err)
	}
	if err := f.proposals.MarkApplied(ctx, proposal.ID, "sol-1", appliedAt); err != nil {
		t.Fatalf("MarkApplied: %v", err)
	}

	baseline := baselinePolicyConfig(0.60)
	g := selfimprove.NewGovernor(f.proposals, f.settings, f.positions,
		fakeShadowBacktestSource{}, baseline,
		selfimprove.WithNow(func() time.Time { return trackingClosed }))

	rolledBack, err := g.TrackAndRollback(ctx, proposal.ID)
	if err != nil {
		t.Fatalf("TrackAndRollback: %v", err)
	}
	if !rolledBack {
		t.Fatalf("TrackAndRollback() = false, want true (Expectancy degraded 30%%, exceeding the 20%% threshold)")
	}

	got, err := f.proposals.Get(ctx, proposal.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != domain.PolicyProposalStatusRolledBack {
		t.Fatalf("Get().Status = %q, want rolled_back", got.Status)
	}
	if got.RolledBackReason == nil || *got.RolledBackReason == "" {
		t.Fatalf("Get().RolledBackReason = %v, want a nonempty reason", got.RolledBackReason)
	}

	raw, ok, err := f.settings.Get(ctx, domain.PolicyKeyLongMinProbability)
	if err != nil {
		t.Fatalf("settings.Get: %v", err)
	}
	if !ok || raw != "0.60" {
		t.Fatalf("runtime_settings[%s] = (%q, %v) after rollback, want (0.60, true) - the pre-apply value", domain.PolicyKeyLongMinProbability, raw, ok)
	}
}

func TestGovernor_TrackAndRollback_NoActionBeforeWindowCloses(t *testing.T) {
	f := newGovernorFixtures(t)
	ctx := context.Background()
	appliedAt := time.Date(2026, 3, 2, 16, 0, 0, 0, time.UTC)

	changesJSON, err := domain.EncodePolicyChanges([]domain.PolicyChange{
		{Key: domain.PolicyKeyLongMinProbability, OldValue: `0.60`, NewValue: `0.65`},
	})
	if err != nil {
		t.Fatalf("EncodePolicyChanges: %v", err)
	}
	proposal, err := f.proposals.Insert(ctx, domain.PolicyProposal{RationaleJSON: `{}`, ProposedChangesJSON: changesJSON})
	if err != nil {
		t.Fatalf("Insert proposal: %v", err)
	}
	if err := f.proposals.MarkApplied(ctx, proposal.ID, "sol-1", appliedAt); err != nil {
		t.Fatalf("MarkApplied: %v", err)
	}

	baseline := baselinePolicyConfig(0.60)
	g := selfimprove.NewGovernor(f.proposals, f.settings, f.positions,
		fakeShadowBacktestSource{}, baseline,
		selfimprove.WithNow(func() time.Time { return appliedAt.Add(24 * time.Hour) })) // well inside the tracking window

	rolledBack, err := g.TrackAndRollback(ctx, proposal.ID)
	if err != nil {
		t.Fatalf("TrackAndRollback: %v", err)
	}
	if rolledBack {
		t.Fatalf("TrackAndRollback() = true, want false while the tracking window has not yet closed")
	}

	got, err := f.proposals.Get(ctx, proposal.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != domain.PolicyProposalStatusApplied {
		t.Fatalf("Get().Status = %q, want still applied", got.Status)
	}
}

func TestGovernor_TrackAndRollback_NoActionWhenExpectancyImproved(t *testing.T) {
	f := newGovernorFixtures(t)
	ctx := context.Background()
	appliedAt := time.Date(2026, 3, 2, 16, 0, 0, 0, time.UTC)
	trackingClosed := appliedAt.Add(10 * 24 * time.Hour)

	f.closePosition(t, appliedAt.Add(-3*24*time.Hour), 500.0)
	f.closePosition(t, appliedAt.Add(3*24*time.Hour), 900.0) // improved, not degraded

	proposal, err := f.proposals.Insert(context.Background(), domain.PolicyProposal{RationaleJSON: `{}`, ProposedChangesJSON: `[{"key":"policy.long.min_probability","old_value":"0.60","new_value":"0.65"}]`})
	if err != nil {
		t.Fatalf("Insert proposal: %v", err)
	}
	if err := f.proposals.MarkApplied(ctx, proposal.ID, "sol-1", appliedAt); err != nil {
		t.Fatalf("MarkApplied: %v", err)
	}

	baseline := baselinePolicyConfig(0.60)
	g := selfimprove.NewGovernor(f.proposals, f.settings, f.positions,
		fakeShadowBacktestSource{}, baseline,
		selfimprove.WithNow(func() time.Time { return trackingClosed }))

	rolledBack, err := g.TrackAndRollback(ctx, proposal.ID)
	if err != nil {
		t.Fatalf("TrackAndRollback: %v", err)
	}
	if rolledBack {
		t.Fatalf("TrackAndRollback() = true, want false when realized Expectancy improved post-apply")
	}
}
