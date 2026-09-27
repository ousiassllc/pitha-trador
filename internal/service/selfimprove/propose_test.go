package selfimprove_test

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/selfimprove"
)

func TestGovernor_ProposeDaily_InsertsProposalForWeakBucket(t *testing.T) {
	f := newGovernorFixtures(t)
	ctx := context.Background()
	baseline := baselinePolicyConfig(0.60)
	source := newUptrendSource(f.instrument.ID, time.Now().UTC(), baseline)
	g := selfimprove.NewGovernor(f.proposals, f.settings, f.positions, source, baseline)

	weakLong := domain.CalibrationMetrics{
		Buckets: []domain.ConfidenceBucket{
			{Range: "0.60-0.70", DirectionAccuracy: 0.40, AvgFutureReturnPct: -0.5, SampleCount: 40},
		},
		ExpectedCalibrationError: 0.02,
		SampleCount:              40,
	}
	healthyShort := domain.CalibrationMetrics{
		Buckets: []domain.ConfidenceBucket{
			{Range: "0.60-0.70", DirectionAccuracy: 0.70, AvgFutureReturnPct: 0.3, SampleCount: 40},
		},
		ExpectedCalibrationError: 0.02, SampleCount: 40,
	}

	proposal, ok, err := g.ProposeDaily(ctx, weakLong, healthyShort)
	if err != nil {
		t.Fatalf("ProposeDaily: %v", err)
	}
	if !ok {
		t.Fatalf("ProposeDaily() ok = false, want true for a weak LONG bucket")
	}
	if proposal.ID == 0 || proposal.Status != domain.PolicyProposalStatusPending {
		t.Fatalf("ProposeDaily() proposal = %+v, want a pending row with a nonzero id", proposal)
	}

	stored, err := f.proposals.Get(ctx, proposal.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	changes, err := domain.ParsePolicyChanges(stored.ProposedChangesJSON)
	if err != nil {
		t.Fatalf("ParsePolicyChanges: %v", err)
	}
	if err := domain.ValidatePolicyChanges(changes); err != nil {
		t.Fatalf("ValidatePolicyChanges(stored proposal) = %v, want nil", err)
	}
}

func TestGovernor_ProposeDaily_NoProposalWhenCalibrationHealthy(t *testing.T) {
	f := newGovernorFixtures(t)
	ctx := context.Background()
	baseline := baselinePolicyConfig(0.60)
	source := newUptrendSource(f.instrument.ID, time.Now().UTC(), baseline)
	g := selfimprove.NewGovernor(f.proposals, f.settings, f.positions, source, baseline)

	healthy := domain.CalibrationMetrics{
		Buckets: []domain.ConfidenceBucket{
			{Range: "0.60-0.70", DirectionAccuracy: 0.70, AvgFutureReturnPct: 0.3, SampleCount: 40},
		},
		ExpectedCalibrationError: 0.02, SampleCount: 40,
	}

	_, ok, err := g.ProposeDaily(ctx, healthy, healthy)
	if err != nil {
		t.Fatalf("ProposeDaily: %v", err)
	}
	if ok {
		t.Fatalf("ProposeDaily() ok = true, want false when both directions are healthy")
	}
}
