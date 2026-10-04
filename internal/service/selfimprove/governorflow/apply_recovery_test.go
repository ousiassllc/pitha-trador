package governorflow_test

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/selfimprove"
)

// #455: a Slack failure after a committed apply must not turn the apply
// into a failure; DailyResult.Applied still reports it.
func TestGovernor_RunDaily_NotifierFailureStillReportsApplied(t *testing.T) {
	f := newGovernorFixtures(t)
	ctx := context.Background()
	base := time.Date(2026, 1, 5, 9, 0, 0, 0, time.UTC)
	now := base.Add(10 * time.Minute)
	baseline := baselinePolicyConfig(0.60)
	ai := newFakeAI(t)
	ai.solBody = solProposesLongMinProbability065
	g := selfimprove.NewGovernor(f.proposals, f.settings, f.positions, newUptrendSource(f.instrument.ID, base, baseline), baseline,
		ai.options(selfimprove.WithNow(func() time.Time { return now }), selfimprove.WithNotifier(failingNotifier{}))...)

	result, err := g.RunDaily(ctx, weakLongCalibration())
	if err != nil {
		t.Fatalf("RunDaily: %v", err)
	}
	if !result.Applied {
		t.Fatalf("DailyResult.Applied = false, want true despite the notifier failure")
	}
	f.wantLongMinProbability(t, "0.65")
}

// #451: a proposal left approved by a failed apply (here with only the
// first of two keys written) is finished by the next RunDaily, enters
// FR-SELFIMPROVE-6 tracking as applied, and blocks a fresh Sol analysis.
func TestGovernor_RunDaily_FinishesApprovedProposalLeftByFailedApply(t *testing.T) {
	f := newGovernorFixtures(t)
	ctx := context.Background()
	now := time.Date(2026, 1, 5, 9, 10, 0, 0, time.UTC)
	changesJSON, err := domain.EncodePolicyChanges([]domain.PolicyChange{
		{Key: domain.PolicyKeyLongMinProbability, OldValue: `0.60`, NewValue: `0.65`},
		{Key: domain.PolicyKeyShortMinProbability, OldValue: `0.60`, NewValue: `0.66`},
	})
	if err != nil {
		t.Fatalf("EncodePolicyChanges: %v", err)
	}
	p, err := f.proposals.Insert(ctx, domain.PolicyProposal{RationaleJSON: `{}`, ProposedChangesJSON: changesJSON})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if err := f.proposals.UpdateReview(ctx, p.ID, domain.PolicyProposalStatusApproved, "opus", `{"verdict":"approve"}`); err != nil {
		t.Fatalf("UpdateReview: %v", err)
	}
	if err := f.settings.Set(ctx, domain.PolicyKeyLongMinProbability, `0.65`, now); err != nil { // 2nd Set never happened
		t.Fatalf("Set: %v", err)
	}

	ai := newFakeAI(t)
	ai.solBody = solProposesLongMinProbability065
	g := selfimprove.NewGovernor(f.proposals, f.settings, f.positions, fakeShadowBacktestSource{}, baselinePolicyConfig(0.60),
		ai.options(selfimprove.WithNow(func() time.Time { return now }))...)

	result, err := g.RunDaily(ctx, weakLongCalibration())
	if err != nil {
		t.Fatalf("RunDaily: %v", err)
	}
	if len(result.RetriedApplied) != 1 || result.RetriedApplied[0] != p.ID {
		t.Fatalf("DailyResult.RetriedApplied = %v, want [%d]", result.RetriedApplied, p.ID)
	}
	if result.Proposal != nil {
		t.Errorf("DailyResult.Proposal = %+v, want no new Sol proposal on a catch-up day", result.Proposal)
	}
	if sol, _ := ai.calls(); sol != 0 {
		t.Errorf("Sol called %d times, want 0", sol)
	}

	got, err := f.proposals.Get(ctx, p.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != domain.PolicyProposalStatusApplied || got.AppliedPolicyVersion == nil || got.AppliedAt == nil {
		t.Fatalf("proposal = status %q version %v appliedAt %v, want applied with version and time", got.Status, got.AppliedPolicyVersion, got.AppliedAt)
	}
	f.wantLongMinProbability(t, "0.65")
	raw, ok, err := f.settings.Get(ctx, domain.PolicyKeyShortMinProbability)
	if err != nil || !ok || raw != "0.66" {
		t.Fatalf("runtime_settings[short.min_probability] = (%q, %v, %v), want (0.66, true, nil)", raw, ok, err)
	}
}

// #452: RuntimePolicy reports the applied version in effect, and nothing
// again once that proposal is rolled back.
func TestRuntimePolicy_AppliedPolicyVersion(t *testing.T) {
	f := newGovernorFixtures(t)
	ctx := context.Background()
	rp := selfimprove.NewRuntimePolicy(f.settings, f.proposals, baselinePolicyConfig(0.60))
	want := func(wantVersion string) {
		t.Helper()
		got, err := rp.AppliedPolicyVersion(ctx)
		if err != nil || got != wantVersion {
			t.Fatalf("AppliedPolicyVersion() = (%q, %v), want %q", got, err, wantVersion)
		}
	}

	want("")
	t0 := time.Date(2026, 3, 2, 16, 0, 0, 0, time.UTC)
	first := f.applyProposal(t, "sol-1", "0.60", "0.65", t0)
	want("sol-1")
	second := f.applyProposal(t, "sol-2", "0.65", "0.70", t0.Add(time.Hour))
	want("sol-2")
	if err := f.proposals.MarkRolledBack(ctx, second, t0.Add(48*time.Hour), "degraded"); err != nil {
		t.Fatalf("MarkRolledBack: %v", err)
	}
	want("sol-1")
	if err := f.proposals.MarkRolledBack(ctx, first, t0.Add(72*time.Hour), "degraded"); err != nil {
		t.Fatalf("MarkRolledBack: %v", err)
	}
	want("")
}
