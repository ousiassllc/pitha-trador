package selfimprove_test

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/selfimprove"
)

func TestGovernor_EvaluateProposal_ApprovesAndAppliesWhenCandidateDoesNotWorsenOutcome(t *testing.T) {
	f := newGovernorFixtures(t)
	ctx := context.Background()
	base := time.Date(2026, 1, 5, 9, 0, 0, 0, time.UTC) // a Monday
	now := base.Add(10 * time.Minute)

	baseline := baselinePolicyConfig(0.60)
	source := newUptrendSource(f.instrument.ID, base, baseline)
	g := selfimprove.NewGovernor(f.proposals, f.settings, f.positions, source, baseline,
		selfimprove.WithNow(func() time.Time { return now }))

	changesJSON, err := domain.EncodePolicyChanges([]domain.PolicyChange{
		{Key: domain.PolicyKeyLongMinProbability, OldValue: `0.60`, NewValue: `0.65`}, // still well under the 0.80-confidence decision
	})
	if err != nil {
		t.Fatalf("EncodePolicyChanges: %v", err)
	}
	proposal, err := f.proposals.Insert(ctx, domain.PolicyProposal{RationaleJSON: `{}`, ProposedChangesJSON: changesJSON})
	if err != nil {
		t.Fatalf("Insert proposal: %v", err)
	}

	approved, err := g.EvaluateProposal(ctx, proposal.ID)
	if err != nil {
		t.Fatalf("EvaluateProposal: %v", err)
	}
	if !approved {
		t.Fatalf("EvaluateProposal() approved = false, want true (candidate does not worsen the outcome)")
	}

	got, err := f.proposals.Get(ctx, proposal.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != domain.PolicyProposalStatusApplied {
		t.Fatalf("Get().Status = %q, want applied", got.Status)
	}
	if got.AppliedPolicyVersion == nil || *got.AppliedPolicyVersion == "" {
		t.Fatalf("Get().AppliedPolicyVersion = %v, want a nonempty version", got.AppliedPolicyVersion)
	}
	if got.BacktestResultJSON == nil {
		t.Fatalf("Get().BacktestResultJSON = nil, want the shadow backtest comparison")
	}

	raw, ok, err := f.settings.Get(ctx, domain.PolicyKeyLongMinProbability)
	if err != nil {
		t.Fatalf("settings.Get: %v", err)
	}
	if !ok || raw != "0.65" {
		t.Fatalf("runtime_settings[%s] = (%q, %v), want (0.65, true)", domain.PolicyKeyLongMinProbability, raw, ok)
	}

	current, err := g.CurrentThresholds(ctx)
	if err != nil {
		t.Fatalf("CurrentThresholds: %v", err)
	}
	if current.Long.MinProbability != 0.65 {
		t.Fatalf("CurrentThresholds().Long.MinProbability = %v, want 0.65", current.Long.MinProbability)
	}
}

func TestGovernor_EvaluateProposal_RejectsWhenCandidateEliminatesEveryTrade(t *testing.T) {
	f := newGovernorFixtures(t)
	ctx := context.Background()
	base := time.Date(2026, 1, 5, 9, 0, 0, 0, time.UTC)
	now := base.Add(10 * time.Minute)

	baseline := baselinePolicyConfig(0.76)
	source := newUptrendSource(f.instrument.ID, base, baseline)
	g := selfimprove.NewGovernor(f.proposals, f.settings, f.positions, source, baseline,
		selfimprove.WithNow(func() time.Time { return now }))

	changesJSON, err := domain.EncodePolicyChanges([]domain.PolicyChange{
		// 0.76 -> 0.81 exceeds the fixture decision's 0.80 confidence:
		// the candidate backtest produces zero trades (Expectancy 0),
		// worse than baseline's positive Expectancy from the uptrend.
		{Key: domain.PolicyKeyLongMinProbability, OldValue: `0.76`, NewValue: `0.81`},
	})
	if err != nil {
		t.Fatalf("EncodePolicyChanges: %v", err)
	}
	proposal, err := f.proposals.Insert(ctx, domain.PolicyProposal{RationaleJSON: `{}`, ProposedChangesJSON: changesJSON})
	if err != nil {
		t.Fatalf("Insert proposal: %v", err)
	}

	approved, err := g.EvaluateProposal(ctx, proposal.ID)
	if err != nil {
		t.Fatalf("EvaluateProposal: %v", err)
	}
	if approved {
		t.Fatalf("EvaluateProposal() approved = true, want false (candidate eliminates every trade)")
	}

	got, err := f.proposals.Get(ctx, proposal.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != domain.PolicyProposalStatusRejected {
		t.Fatalf("Get().Status = %q, want rejected", got.Status)
	}
	if got.AppliedAt != nil {
		t.Fatalf("Get().AppliedAt = %v, want nil (a rejected proposal is never applied)", got.AppliedAt)
	}
	if _, ok, err := f.settings.Get(ctx, domain.PolicyKeyLongMinProbability); err != nil || ok {
		t.Fatalf("runtime_settings[%s] set = %v (err %v), want unset after rejection", domain.PolicyKeyLongMinProbability, ok, err)
	}
}

func TestGovernor_EvaluateProposal_RejectsAndNeverWritesRiskOrPromptVersionKeys(t *testing.T) {
	f := newGovernorFixtures(t)
	ctx := context.Background()
	baseline := baselinePolicyConfig(0.60)
	source := newUptrendSource(f.instrument.ID, time.Now().UTC(), baseline)
	g := selfimprove.NewGovernor(f.proposals, f.settings, f.positions, source, baseline)

	for _, key := range []string{"risk.max_position_size", "risk.daily_loss_limit_jpy", "jev.prompt_version"} {
		changesJSON, err := domain.EncodePolicyChanges([]domain.PolicyChange{
			{Key: key, OldValue: `1`, NewValue: `2`},
		})
		if err != nil {
			t.Fatalf("EncodePolicyChanges: %v", err)
		}
		// Insert bypasses Governor.ProposeDaily's own validation, simulating
		// a malformed/tampered proposal reaching EvaluateProposal directly -
		// FR-SELFIMPROVE-2's guarantee must hold even then.
		proposal, err := f.proposals.Insert(ctx, domain.PolicyProposal{RationaleJSON: `{}`, ProposedChangesJSON: changesJSON})
		if err != nil {
			t.Fatalf("Insert proposal(%s): %v", key, err)
		}

		if _, err := g.EvaluateProposal(ctx, proposal.ID); err == nil {
			t.Fatalf("EvaluateProposal(key=%q) error = nil, want a validation error", key)
		}

		if _, ok, err := f.settings.Get(ctx, key); err != nil || ok {
			t.Fatalf("runtime_settings[%s] set = %v (err %v), want unset - Governor must never write it", key, ok, err)
		}
	}
}
