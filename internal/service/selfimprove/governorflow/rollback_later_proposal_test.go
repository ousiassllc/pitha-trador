package governorflow_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/selfimprove"
)

// applyProposal inserts a proposal changing policy.long.min_probability
// old -> new, applies it at appliedAt (runtime_settings + status) the way
// Governor.apply does, and returns its id.
func (f governorFixtures) applyProposal(t *testing.T, version, oldValue, newValue string, appliedAt time.Time) int64 {
	t.Helper()
	ctx := context.Background()
	changesJSON, err := domain.EncodePolicyChanges([]domain.PolicyChange{
		{Key: domain.PolicyKeyLongMinProbability, OldValue: oldValue, NewValue: newValue},
	})
	if err != nil {
		t.Fatalf("EncodePolicyChanges: %v", err)
	}
	p, err := f.proposals.Insert(ctx, domain.PolicyProposal{RationaleJSON: `{}`, ProposedChangesJSON: changesJSON})
	if err != nil {
		t.Fatalf("Insert proposal: %v", err)
	}
	if err := f.settings.Set(ctx, domain.PolicyKeyLongMinProbability, newValue, appliedAt); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := f.proposals.MarkApplied(ctx, p.ID, version, appliedAt); err != nil {
		t.Fatalf("MarkApplied: %v", err)
	}
	return p.ID
}

func (f governorFixtures) wantLongMinProbability(t *testing.T, want string) {
	t.Helper()
	raw, ok, err := f.settings.Get(context.Background(), domain.PolicyKeyLongMinProbability)
	if err != nil {
		t.Fatalf("settings.Get: %v", err)
	}
	if !ok || raw != want {
		t.Fatalf("runtime_settings[%s] = (%q, %v), want (%s, true)", domain.PolicyKeyLongMinProbability, raw, ok, want)
	}
}

// degradedFixtures returns a Governor whose tracking windows have closed
// for proposals applied on/after appliedAt and whose realized Expectancy
// degraded for every one of them (a profit before appliedAt, losses after).
func degradedFixtures(t *testing.T, f governorFixtures, appliedAt time.Time) *selfimprove.Governor {
	t.Helper()
	f.closePosition(t, appliedAt.Add(-3*24*time.Hour), 1000)
	f.closePosition(t, appliedAt.Add(3*24*time.Hour), -1000)
	now := appliedAt.Add(30 * 24 * time.Hour)
	return selfimprove.NewGovernor(f.proposals, f.settings, f.positions,
		fakeShadowBacktestSource{}, baselinePolicyConfig(0.60),
		selfimprove.WithNow(func() time.Time { return now }))
}

// #450: rolling back an earlier proposal must not overwrite the value a
// later proposal applied to the same key.
func TestGovernor_TrackAndRollback_KeepsLaterProposalsAppliedValue(t *testing.T) {
	f := newGovernorFixtures(t)
	ctx := context.Background()
	appliedAt := time.Date(2026, 3, 2, 16, 0, 0, 0, time.UTC)
	first := f.applyProposal(t, "sol-1", "0.60", "0.65", appliedAt)
	second := f.applyProposal(t, "sol-2", "0.65", "0.70", appliedAt.Add(24*time.Hour))
	g := degradedFixtures(t, f, appliedAt)

	rolledBack, err := g.TrackAndRollback(ctx, first)
	if err != nil {
		t.Fatalf("TrackAndRollback: %v", err)
	}
	if !rolledBack {
		t.Fatalf("TrackAndRollback(first) = false, want true")
	}
	f.wantLongMinProbability(t, "0.70") // sol-2's value survives

	got, err := f.proposals.Get(ctx, first)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.RolledBackReason == nil || !strings.Contains(*got.RolledBackReason, domain.PolicyKeyLongMinProbability) {
		t.Errorf("RolledBackReason = %v, want it to name the key left unchanged", got.RolledBackReason)
	}

	// Rolling back the later proposal must not resurrect the rolled-back
	// proposal's 0.65: it falls through to the pre-sol-1 0.60.
	if rolledBack, err := g.TrackAndRollback(ctx, second); err != nil || !rolledBack {
		t.Fatalf("TrackAndRollback(second) = (%v, %v), want (true, nil)", rolledBack, err)
	}
	f.wantLongMinProbability(t, "0.60")
}

// Rolling back the later proposal first, then the earlier one, restores
// each step in turn.
func TestGovernor_TrackAndRollback_LaterThenEarlierProposal(t *testing.T) {
	f := newGovernorFixtures(t)
	ctx := context.Background()
	appliedAt := time.Date(2026, 3, 2, 16, 0, 0, 0, time.UTC)
	first := f.applyProposal(t, "sol-1", "0.60", "0.65", appliedAt)
	second := f.applyProposal(t, "sol-2", "0.65", "0.70", appliedAt.Add(24*time.Hour))
	g := degradedFixtures(t, f, appliedAt)

	if rolledBack, err := g.TrackAndRollback(ctx, second); err != nil || !rolledBack {
		t.Fatalf("TrackAndRollback(second) = (%v, %v), want (true, nil)", rolledBack, err)
	}
	f.wantLongMinProbability(t, "0.65")
	if rolledBack, err := g.TrackAndRollback(ctx, first); err != nil || !rolledBack {
		t.Fatalf("TrackAndRollback(first) = (%v, %v), want (true, nil)", rolledBack, err)
	}
	f.wantLongMinProbability(t, "0.60")
}

// #455: a Slack failure after a committed apply/rollback is logged, not
// reported as a failed apply/rollback.
type failingNotifier struct{ selfimprove.NoopNotifier }

func (failingNotifier) ProposalApplied(context.Context, domain.PolicyProposal) error {
	return context.DeadlineExceeded
}

func (failingNotifier) ProposalRolledBack(context.Context, domain.PolicyProposal, string) error {
	return context.DeadlineExceeded
}

func TestGovernor_TrackAndRollback_NotifierFailureStillReportsRollback(t *testing.T) {
	f := newGovernorFixtures(t)
	ctx := context.Background()
	appliedAt := time.Date(2026, 3, 2, 16, 0, 0, 0, time.UTC)
	id := f.applyProposal(t, "sol-1", "0.60", "0.65", appliedAt)
	f.closePosition(t, appliedAt.Add(-3*24*time.Hour), 1000)
	f.closePosition(t, appliedAt.Add(3*24*time.Hour), -1000)
	now := appliedAt.Add(30 * 24 * time.Hour)
	g := selfimprove.NewGovernor(f.proposals, f.settings, f.positions, fakeShadowBacktestSource{}, baselinePolicyConfig(0.60),
		selfimprove.WithNow(func() time.Time { return now }), selfimprove.WithNotifier(failingNotifier{}))

	result, err := g.RunDaily(ctx, &fixedCalibration{})
	if err != nil {
		t.Fatalf("RunDaily: %v", err)
	}
	if len(result.RolledBack) != 1 || result.RolledBack[0] != id {
		t.Fatalf("DailyResult.RolledBack = %v, want [%d]", result.RolledBack, id)
	}
	f.wantLongMinProbability(t, "0.60")
}
