package governorflow_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/selfimprove"
)

var monday = time.Date(2026, 1, 5, 9, 0, 0, 0, time.UTC)

func reviewOf(t *testing.T, p domain.PolicyProposal) map[string]any {
	t.Helper()
	if p.ReviewJSON == nil {
		t.Fatalf("proposal %d has no review_json", p.ID)
	}
	var review map[string]any
	if err := json.Unmarshal([]byte(*p.ReviewJSON), &review); err != nil {
		t.Fatalf("unmarshal review_json: %v", err)
	}
	return review
}

// FR-SELFIMPROVE-8: the LLM's output is machine-validated; a violation is
// rejected as llm_output_out_of_bounds and never reaches the backtest/Opus.
func TestGovernor_ProposeDaily_OutOfBoundsLLMOutputIsRejectedAndNeverReviewed(t *testing.T) {
	tests := map[string]string{
		"risk key":              `{"proposed_changes":[{"key":"risk.max_position_size","new_value":2}]}`,
		"prompt version key":    `{"proposed_changes":[{"key":"jev.prompt_version","new_value":2}]}`,
		"unknown policy key":    `{"proposed_changes":[{"key":"policy.long.nonexistent","new_value":0.5}]}`,
		"change wider than cap": `{"proposed_changes":[{"key":"policy.long.min_probability","new_value":0.80}]}`,
		"value above 1":         `{"proposed_changes":[{"key":"policy.long.min_probability","new_value":1.02}]}`,
		"duplicate key":         `{"proposed_changes":[{"key":"policy.long.min_probability","new_value":0.62},{"key":"policy.long.min_probability","new_value":0.64}]}`,
		"non numeric value":     `{"proposed_changes":[{"key":"policy.long.min_probability","new_value":"high"}]}`,
		"entry quality 2 steps": `{"proposed_changes":[{"key":"policy.long.min_entry_quality","new_value":"fair"}]}`,
		"one bad among good":    `{"proposed_changes":[{"key":"policy.long.min_probability","new_value":0.62},{"key":"risk.daily_loss_limit_jpy","new_value":1}]}`,
	}
	for name, solAnswer := range tests {
		t.Run(name, func(t *testing.T) {
			f := newGovernorFixtures(t)
			ctx := context.Background()
			baseline := baselinePolicyConfig(0.60)
			ai := newFakeAI(t)
			ai.solBody = solAnswer
			g := selfimprove.NewGovernor(f.proposals, f.settings, f.positions,
				newUptrendSource(f.instrument.ID, monday, baseline), baseline,
				ai.options(selfimprove.WithNow(func() time.Time { return monday.Add(10 * time.Minute) }))...)

			result, err := g.RunDaily(ctx, weakLongCalibration())
			if err != nil {
				t.Fatalf("RunDaily: %v", err)
			}
			if result.Proposal == nil || result.Proposal.Status != domain.PolicyProposalStatusRejected || result.Applied {
				t.Fatalf("RunDaily() = %+v, want a recorded status=rejected proposal, nothing applied", result)
			}
			stored, err := f.proposals.Get(ctx, result.Proposal.ID)
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			if stored.Status != domain.PolicyProposalStatusRejected {
				t.Errorf("stored status = %q, want rejected", stored.Status)
			}
			if reason := reviewOf(t, stored)["reason"]; reason != selfimprove.ReasonLLMOutputOutOfBounds {
				t.Errorf("review_json.reason = %v, want %q", reason, selfimprove.ReasonLLMOutputOutOfBounds)
			}
			if _, opusCalls := ai.calls(); opusCalls != 0 {
				t.Errorf("Opus API called %d time(s) for an out-of-bounds proposal, want 0", opusCalls)
			}
			for _, key := range []string{"risk.max_position_size", "risk.daily_loss_limit_jpy", "jev.prompt_version", domain.PolicyKeyLongMinProbability} {
				if _, ok, err := f.settings.Get(ctx, key); err != nil || ok {
					t.Errorf("runtime_settings[%s] set = %v (err %v), want untouched", key, ok, err)
				}
			}
		})
	}
}

// FR-SELFIMPROVE-9: Opus can veto a proposal that meets the deterministic thresholds.
func TestGovernor_RunDaily_OpusAPIRejectVetoesThresholdPassingProposal(t *testing.T) {
	f := newGovernorFixtures(t)
	ctx := context.Background()
	baseline := baselinePolicyConfig(0.60)
	ai := newFakeAI(t)
	ai.solBody = solProposesLongMinProbability065
	ai.opusBody = `{"verdict":"reject","reason":"regime looks transient"}`
	g := selfimprove.NewGovernor(f.proposals, f.settings, f.positions,
		newUptrendSource(f.instrument.ID, monday, baseline), baseline,
		ai.options(selfimprove.WithNow(func() time.Time { return monday.Add(10 * time.Minute) }))...)

	result, err := g.RunDaily(ctx, weakLongCalibration())
	if err != nil {
		t.Fatalf("RunDaily: %v", err)
	}
	if result.Applied {
		t.Fatal("proposal applied although Opus rejected it")
	}
	stored, _ := f.proposals.Get(ctx, result.Proposal.ID)
	review := reviewOf(t, stored)
	if stored.Status != domain.PolicyProposalStatusRejected || review["reason"] != "regime looks transient" || review["deterministic_passed"] != true {
		t.Errorf("stored = %s / review %v, want rejected by Opus with deterministic_passed=true", stored.Status, review)
	}
	if _, ok, _ := f.settings.Get(ctx, domain.PolicyKeyLongMinProbability); ok {
		t.Error("runtime_settings changed for a rejected proposal")
	}
}

// FR-SELFIMPROVE-9: an Opus "approve" can never rescue a proposal that misses the deterministic thresholds.
func TestGovernor_EvaluateProposal_OpusApproveCannotOverrideDeterministicThresholds(t *testing.T) {
	f := newGovernorFixtures(t)
	ctx := context.Background()
	baseline := baselinePolicyConfig(0.76)
	ai := newFakeAI(t) // Opus approves everything it is asked about
	g := selfimprove.NewGovernor(f.proposals, f.settings, f.positions,
		newUptrendSource(f.instrument.ID, monday, baseline), baseline,
		ai.options(selfimprove.WithNow(func() time.Time { return monday.Add(10 * time.Minute) }))...)

	changesJSON, _ := domain.EncodePolicyChanges([]domain.PolicyChange{{Key: domain.PolicyKeyLongMinProbability, OldValue: `0.76`, NewValue: `0.81`}})
	p, err := f.proposals.Insert(ctx, domain.PolicyProposal{RationaleJSON: `{}`, ProposedChangesJSON: changesJSON})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}

	approved, err := g.EvaluateProposal(ctx, p.ID)
	if err != nil || approved {
		t.Fatalf("EvaluateProposal = (%v, %v), want a rejection", approved, err)
	}
	if _, opusCalls := ai.calls(); opusCalls != 0 {
		t.Errorf("Opus API called %d time(s), want 0: the deterministic thresholds already failed", opusCalls)
	}
	got, _ := f.proposals.Get(ctx, p.ID)
	if got.Status != domain.PolicyProposalStatusRejected {
		t.Errorf("status = %q, want rejected", got.Status)
	}
}

// overview.md §8: an Opus API failure skips the review (proposal stays
// pending), notifies, and the next business day's run retries it.
func TestGovernor_RunDaily_OpusAPIFailureLeavesProposalPendingAndRetriesNextDay(t *testing.T) {
	f := newGovernorFixtures(t)
	ctx := context.Background()
	baseline := baselinePolicyConfig(0.60)
	ai := newFakeAI(t)
	ai.solBody = solProposesLongMinProbability065
	ai.opusStatus = 503
	notifier := &recordingNotifier{}
	now := monday.Add(10 * time.Minute)
	g := selfimprove.NewGovernor(f.proposals, f.settings, f.positions,
		newUptrendSource(f.instrument.ID, monday, baseline), baseline,
		ai.options(selfimprove.WithNotifier(notifier), selfimprove.WithNow(func() time.Time { return now }))...)

	result, err := g.RunDaily(ctx, weakLongCalibration())
	if err != nil {
		t.Fatalf("RunDaily: %v, want a skipped stage, not an error", err)
	}
	if result.Applied || len(result.SkippedStages) != 1 || result.SkippedStages[0] != "opus" {
		t.Fatalf("RunDaily() = %+v, want Opus skipped and nothing applied", result)
	}
	if len(notifier.skipped) != 1 || notifier.skipped[0] != "opus" {
		t.Errorf("notifications = %v, want one opus skip", notifier.skipped)
	}
	stored, _ := f.proposals.Get(ctx, result.Proposal.ID)
	if stored.Status != domain.PolicyProposalStatusPending {
		t.Fatalf("status = %q, want pending until Opus is reachable", stored.Status)
	}

	// Next business day: Opus is back. The pending proposal is reviewed and
	// no second Sol proposal is created on top of it.
	ai.mu.Lock()
	ai.opusStatus = 0
	ai.mu.Unlock()
	solBefore, _ := ai.calls()
	now = now.Add(24 * time.Hour)
	result, err = g.RunDaily(ctx, weakLongCalibration())
	if err != nil {
		t.Fatalf("second RunDaily: %v", err)
	}
	if len(result.RetriedApplied) != 1 || result.RetriedApplied[0] != stored.ID {
		t.Fatalf("second RunDaily() = %+v, want the pending proposal retried and applied", result)
	}
	if solAfter, _ := ai.calls(); solAfter != solBefore {
		t.Errorf("Sol called again (%d -> %d) while a proposal was still pending", solBefore, solAfter)
	}
	got, _ := f.proposals.Get(ctx, stored.ID)
	if got.Status != domain.PolicyProposalStatusApplied {
		t.Errorf("status = %q after retry, want applied", got.Status)
	}
}

func TestGovernor_RunDaily_SolAPIFailureIsSkippedAndNotified(t *testing.T) {
	f := newGovernorFixtures(t)
	baseline := baselinePolicyConfig(0.60)
	ai := newFakeAI(t)
	ai.solStatus = 500
	notifier := &recordingNotifier{}
	g := selfimprove.NewGovernor(f.proposals, f.settings, f.positions,
		newUptrendSource(f.instrument.ID, monday, baseline), baseline, ai.options(selfimprove.WithNotifier(notifier))...)

	result, err := g.RunDaily(context.Background(), weakLongCalibration())
	if err != nil {
		t.Fatalf("RunDaily: %v, want a skipped stage, not an error", err)
	}
	if result.Proposal != nil || len(result.SkippedStages) != 1 || result.SkippedStages[0] != "sol" {
		t.Errorf("RunDaily() = %+v, want Sol skipped, no proposal", result)
	}
	if len(notifier.skipped) != 1 || notifier.skipped[0] != "sol" {
		t.Errorf("notifications = %v, want one sol skip", notifier.skipped)
	}
}

// Without SOL_*/OPUS_* configured (the default Governor) every stage is
// skipped quietly and nothing is ever proposed or approved.
func TestGovernor_RunDaily_UnconfiguredAIIsSkippedWithoutNotification(t *testing.T) {
	f := newGovernorFixtures(t)
	baseline := baselinePolicyConfig(0.60)
	notifier := &recordingNotifier{}
	g := selfimprove.NewGovernor(f.proposals, f.settings, f.positions,
		newUptrendSource(f.instrument.ID, monday, baseline), baseline, selfimprove.WithNotifier(notifier))

	result, err := g.RunDaily(context.Background(), weakLongCalibration())
	if err != nil {
		t.Fatalf("RunDaily: %v", err)
	}
	if result.Proposal != nil || result.Applied || len(result.SkippedStages) != 1 {
		t.Errorf("RunDaily() = %+v, want Sol skipped and nothing proposed", result)
	}
	if len(notifier.skipped) != 0 {
		t.Errorf("notifications = %v, want none for an unconfigured API", notifier.skipped)
	}
}
