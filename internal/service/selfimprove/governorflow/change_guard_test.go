package governorflow_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/changeguard"
	"github.com/ousiassllc/pitha-trador/internal/service/selfimprove"
)

type fakeModelAdoptions struct {
	at    time.Time
	found bool
}

func (f fakeModelAdoptions) LastAdoptedAt(context.Context) (time.Time, bool, error) {
	return f.at, f.found, nil
}

// guardedGovernor evaluates proposals at a mutable clock with the default
// change limits and the given model-adoption history.
func guardedGovernor(t *testing.T, f governorFixtures, base time.Time, clock *time.Time, models fakeModelAdoptions) *selfimprove.Governor {
	t.Helper()
	baseline := baselinePolicyConfig(0.60)
	return selfimprove.NewGovernor(f.proposals, f.settings, f.positions, newUptrendSource(f.instrument.ID, base, baseline), baseline,
		newFakeAI(t).options(selfimprove.WithNow(func() time.Time { return *clock }),
			selfimprove.WithChangeGuard(changeguard.DefaultLimits(), models))...)
}

func insertMinProbabilityProposal(t *testing.T, f governorFixtures, from, to string) int64 {
	t.Helper()
	changes, err := domain.EncodePolicyChanges([]domain.PolicyChange{{Key: domain.PolicyKeyLongMinProbability, OldValue: from, NewValue: to}})
	if err != nil {
		t.Fatalf("EncodePolicyChanges: %v", err)
	}
	p, err := f.proposals.Insert(context.Background(), domain.PolicyProposal{RationaleJSON: `{}`, ProposedChangesJSON: changes})
	if err != nil {
		t.Fatalf("Insert proposal: %v", err)
	}
	return p.ID
}

// rejectedReason returns the review reason of a proposal the Governor rejected.
func rejectedReason(t *testing.T, f governorFixtures, id int64) (reason, reviewedBy string) {
	t.Helper()
	p, err := f.proposals.Get(context.Background(), id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if p.Status != domain.PolicyProposalStatusRejected || p.ReviewJSON == nil || p.ReviewedBy == nil {
		t.Fatalf("proposal %d = status %q review %v, want rejected with a review", id, p.Status, p.ReviewJSON)
	}
	var review struct{ Reason, Detail string }
	if err := json.Unmarshal([]byte(*p.ReviewJSON), &review); err != nil || review.Detail == "" {
		t.Fatalf("review_json = %s (%v), want a reason and a detail", *p.ReviewJSON, err)
	}
	return review.Reason, *p.ReviewedBy
}

func minProbabilitySetting(t *testing.T, f governorFixtures) string {
	t.Helper()
	raw, _, err := f.settings.Get(context.Background(), domain.PolicyKeyLongMinProbability)
	if err != nil {
		t.Fatalf("settings.Get: %v", err)
	}
	return raw
}

// FR-SELFIMPROVE-10: a second policy.* change inside the rate-limit window is
// not applied and the reason is recorded; after the window it is.
func TestGovernor_RateLimitsPolicyChangesAndRecordsTheReason(t *testing.T) {
	f := newGovernorFixtures(t)
	ctx := context.Background()
	base := time.Date(2026, 1, 5, 9, 0, 0, 0, time.UTC) // Monday
	clock := base.Add(10 * time.Minute)
	g := guardedGovernor(t, f, base, &clock, fakeModelAdoptions{})

	first := insertMinProbabilityProposal(t, f, `0.60`, `0.65`)
	if applied, err := g.EvaluateProposal(ctx, first); err != nil || !applied {
		t.Fatalf("first EvaluateProposal = %v, %v, want applied", applied, err)
	}

	clock = base.AddDate(0, 0, 2) // Wednesday: 2 of 5 trading days
	second := insertMinProbabilityProposal(t, f, `0.65`, `0.70`)
	applied, err := g.EvaluateProposal(ctx, second)
	if err != nil || applied {
		t.Fatalf("second EvaluateProposal = %v, %v, want blocked", applied, err)
	}
	if reason, by := rejectedReason(t, f, second); reason != changeguard.ReasonPolicyChangeRateLimited || by != "governor" {
		t.Errorf("reason = %q by %q, want %s by the governor", reason, by, changeguard.ReasonPolicyChangeRateLimited)
	}
	if got := minProbabilitySetting(t, f); got != "0.65" {
		t.Errorf("min_probability = %s, want the first change (0.65) untouched", got)
	}

	clock = base.AddDate(0, 0, 8) // Tuesday of the next week: 5 trading days after the first apply
	third := insertMinProbabilityProposal(t, f, `0.65`, `0.70`)
	if applied, err := g.EvaluateProposal(ctx, third); err != nil || !applied {
		t.Fatalf("third EvaluateProposal = %v, %v, want applied after the window", applied, err)
	}
}

// A model adopted shortly before freezes policy.* changes; the proposal is
// rejected with the reason, and applied once the freeze is over.
func TestGovernor_FreezesPolicyChangesAfterAModelAdoption(t *testing.T) {
	f := newGovernorFixtures(t)
	ctx := context.Background()
	base := time.Date(2026, 1, 6, 9, 0, 0, 0, time.UTC)
	clock := base.Add(10 * time.Minute)
	adoptedAt := base.Add(-18 * time.Hour) // the previous evening, 1 trading day ago
	g := guardedGovernor(t, f, base, &clock, fakeModelAdoptions{at: adoptedAt, found: true})

	frozen := insertMinProbabilityProposal(t, f, `0.60`, `0.65`)
	if applied, err := g.EvaluateProposal(ctx, frozen); err != nil || applied {
		t.Fatalf("EvaluateProposal = %v, %v, want blocked by the freeze", applied, err)
	}
	if reason, _ := rejectedReason(t, f, frozen); reason != changeguard.ReasonModelAdoptionFreeze {
		t.Errorf("reason = %q, want %s", reason, changeguard.ReasonModelAdoptionFreeze)
	}
	if got := minProbabilitySetting(t, f); got != "" {
		t.Errorf("min_probability = %q, want unchanged", got)
	}

	clock = base.AddDate(0, 0, 3)
	ok := insertMinProbabilityProposal(t, f, `0.60`, `0.65`)
	if applied, err := g.EvaluateProposal(ctx, ok); err != nil || !applied {
		t.Fatalf("EvaluateProposal after the freeze = %v, %v, want applied", applied, err)
	}
}

// A governor without WithChangeGuard applies every approved proposal as before.
func TestGovernor_WithoutChangeGuardAppliesBackToBackChanges(t *testing.T) {
	f := newGovernorFixtures(t)
	ctx := context.Background()
	base := time.Date(2026, 1, 5, 9, 0, 0, 0, time.UTC)
	baseline := baselinePolicyConfig(0.60)
	g := selfimprove.NewGovernor(f.proposals, f.settings, f.positions, newUptrendSource(f.instrument.ID, base, baseline), baseline,
		newFakeAI(t).options(selfimprove.WithNow(func() time.Time { return base.Add(10 * time.Minute) }))...)
	for _, step := range [][2]string{{`0.60`, `0.65`}, {`0.65`, `0.70`}} {
		id := insertMinProbabilityProposal(t, f, step[0], step[1])
		if applied, err := g.EvaluateProposal(ctx, id); err != nil || !applied {
			t.Fatalf("EvaluateProposal(%s -> %s) = %v, %v, want applied", step[0], step[1], applied, err)
		}
	}
}
