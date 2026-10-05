package governorflow_test

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/assist"
	"github.com/ousiassllc/pitha-trador/internal/service/selfimprove"
)

// With no shadow-backtest trades on either side, "Expectancy 0 >= 0 and
// MaxDD 0 <= 0" must not read as "no degradation": the proposal is rejected
// as insufficient_samples and Opus is never consulted.
func TestGovernor_EvaluateProposal_RejectsWhenShadowBacktestHasNoTrades(t *testing.T) {
	f := newGovernorFixtures(t)
	ctx := context.Background()
	baseline := baselinePolicyConfig(0.60)
	ai := newFakeAI(t) // Opus would approve anything it is asked about
	g := selfimprove.NewGovernor(f.proposals, f.settings, f.positions, fakeShadowBacktestSource{}, baseline,
		ai.options(selfimprove.WithNow(func() time.Time { return monday.Add(10 * time.Minute) }))...)

	changesJSON, _ := domain.EncodePolicyChanges([]domain.PolicyChange{{Key: domain.PolicyKeyLongMinProbability, OldValue: `0.60`, NewValue: `0.65`}})
	p, err := f.proposals.Insert(ctx, domain.PolicyProposal{RationaleJSON: `{}`, ProposedChangesJSON: changesJSON})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}

	approved, err := g.EvaluateProposal(ctx, p.ID)
	if err != nil || approved {
		t.Fatalf("EvaluateProposal = (%v, %v), want a rejection", approved, err)
	}
	stored, _ := f.proposals.Get(ctx, p.ID)
	review := reviewOf(t, stored)
	if stored.Status != domain.PolicyProposalStatusRejected || review["sufficient_samples"] != false {
		t.Errorf("stored = %s / review %v, want rejected with sufficient_samples=false", stored.Status, review)
	}
	if reason, _ := review["reason"].(string); len(reason) < len(assist.ReasonInsufficientSamples) || reason[:len(assist.ReasonInsufficientSamples)] != assist.ReasonInsufficientSamples {
		t.Errorf("review reason = %q, want prefix %q", reason, assist.ReasonInsufficientSamples)
	}
	if _, opusCalls := ai.calls(); opusCalls != 0 {
		t.Errorf("Opus API called %d time(s), want 0", opusCalls)
	}
	if _, ok, _ := f.settings.Get(ctx, domain.PolicyKeyLongMinProbability); ok {
		t.Error("runtime_settings changed for an evidence-free proposal")
	}
}

// Below MinCalibrationSamples Sol is not consulted: nothing is proposed and,
// since no AI API failed, nothing is reported as a skipped stage either.
func TestGovernor_RunDaily_SkipsSolWhenTooFewCalibrationSamples(t *testing.T) {
	f := newGovernorFixtures(t)
	ctx := context.Background()
	baseline := baselinePolicyConfig(0.60)
	ai := newFakeAI(t)
	ai.solBody = solProposesLongMinProbability065
	g := selfimprove.NewGovernor(f.proposals, f.settings, f.positions,
		newUptrendSource(f.instrument.ID, monday, baseline), baseline,
		ai.options(selfimprove.WithNow(func() time.Time { return monday.Add(10 * time.Minute) }))...)

	sparse := weakLongCalibration()
	sparse.long.SampleCount = selfimprove.MinCalibrationSamples/2 - 1
	sparse.short.SampleCount = selfimprove.MinCalibrationSamples / 2

	result, err := g.RunDaily(ctx, sparse)
	if err != nil {
		t.Fatalf("RunDaily: %v", err)
	}
	if result.Proposal != nil || len(result.SkippedStages) != 0 {
		t.Errorf("RunDaily() = %+v, want no proposal and no SkippedStages", result)
	}
	if solCalls, _ := ai.calls(); solCalls != 0 {
		t.Errorf("Sol API called %d time(s), want 0", solCalls)
	}

	sparse.short.SampleCount++ // exactly MinCalibrationSamples in total
	result, err = g.RunDaily(ctx, sparse)
	if err != nil || result.Proposal == nil {
		t.Fatalf("RunDaily at the minimum = (%+v, %v), want a Sol proposal", result, err)
	}
}
