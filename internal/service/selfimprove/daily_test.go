package selfimprove_test

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/selfimprove"
)

// fixedCalibration returns the same per-direction metrics regardless of
// since, recording the since it was asked for.
type fixedCalibration struct {
	long, short domain.CalibrationMetrics
	since       time.Time
}

func (c *fixedCalibration) DirectionMetricsSince(_ context.Context, since time.Time) (domain.CalibrationMetrics, domain.CalibrationMetrics, error) {
	c.since = since
	return c.long, c.short, nil
}

func weakLongCalibration() *fixedCalibration {
	return &fixedCalibration{
		long: domain.CalibrationMetrics{
			Buckets:                  []domain.ConfidenceBucket{{Range: "0.60-0.70", DirectionAccuracy: 0.40, AvgFutureReturnPct: -0.5, SampleCount: 40}},
			ExpectedCalibrationError: 0.02, SampleCount: 40,
		},
		short: domain.CalibrationMetrics{
			Buckets:                  []domain.ConfidenceBucket{{Range: "0.60-0.70", DirectionAccuracy: 0.70, AvgFutureReturnPct: 0.3, SampleCount: 40}},
			ExpectedCalibrationError: 0.02, SampleCount: 40,
		},
	}
}

func TestGovernor_RunDaily_ProposesEvaluatesAndAppliesToRuntimePolicy(t *testing.T) {
	f := newGovernorFixtures(t)
	ctx := context.Background()
	base := time.Date(2026, 1, 5, 9, 0, 0, 0, time.UTC) // a Monday
	now := base.Add(10 * time.Minute)
	baseline := baselinePolicyConfig(0.60)
	ai := newFakeAI(t)
	ai.solBody = solProposesLongMinProbability065
	g := selfimprove.NewGovernor(f.proposals, f.settings, f.positions, newUptrendSource(f.instrument.ID, base, baseline), baseline,
		ai.options(selfimprove.WithNow(func() time.Time { return now }))...)

	calibration := weakLongCalibration()
	result, err := g.RunDaily(ctx, calibration)
	if err != nil {
		t.Fatalf("RunDaily: %v", err)
	}
	if result.Proposal == nil {
		t.Fatalf("RunDaily() Proposal = nil, want a proposal for the weak LONG bucket")
	}
	if !calibration.since.Before(now) {
		t.Errorf("calibration since = %v, want a lookback window before now %v", calibration.since, now)
	}

	stored, err := f.proposals.Get(ctx, result.Proposal.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if result.Applied != (stored.Status == domain.PolicyProposalStatusApplied) {
		t.Fatalf("RunDaily() Applied = %v but stored status = %q", result.Applied, stored.Status)
	}
	if stored.Status == domain.PolicyProposalStatusPending {
		t.Fatalf("stored status = pending, want the proposal evaluated in the same batch")
	}

	current, err := selfimprove.NewRuntimePolicy(f.settings, baseline).CurrentThresholds(ctx)
	if err != nil {
		t.Fatalf("CurrentThresholds: %v", err)
	}
	if result.Applied && current == baseline {
		t.Errorf("RuntimePolicy thresholds unchanged after an applied proposal: %+v", current)
	}
	if !result.Applied && current != baseline {
		t.Errorf("RuntimePolicy thresholds = %+v, want baseline after a rejected proposal", current)
	}
}

func TestGovernor_RunDaily_NoProposalWhenCalibrationHealthy(t *testing.T) {
	f := newGovernorFixtures(t)
	baseline := baselinePolicyConfig(0.60)
	g := selfimprove.NewGovernor(f.proposals, f.settings, f.positions, newUptrendSource(f.instrument.ID, time.Now().UTC(), baseline), baseline, newFakeAI(t).options()...)

	healthy := weakLongCalibration()
	healthy.long = healthy.short
	result, err := g.RunDaily(context.Background(), healthy)
	if err != nil {
		t.Fatalf("RunDaily: %v", err)
	}
	if result.Proposal != nil || result.Applied {
		t.Errorf("RunDaily() = %+v, want no proposal for healthy calibration", result)
	}
}

func TestRuntimePolicy_CurrentThresholds_OverridesBaselineWithRuntimeSettings(t *testing.T) {
	f := newGovernorFixtures(t)
	ctx := context.Background()
	baseline := baselinePolicyConfig(0.60)
	if err := f.settings.Set(ctx, domain.PolicyKeyLongMinProbability, "0.72", time.Now().UTC()); err != nil {
		t.Fatalf("settings.Set: %v", err)
	}

	current, err := selfimprove.NewRuntimePolicy(f.settings, baseline).CurrentThresholds(ctx)
	if err != nil {
		t.Fatalf("CurrentThresholds: %v", err)
	}
	if current.Long.MinProbability != 0.72 {
		t.Errorf("Long.MinProbability = %v, want the runtime_settings override 0.72", current.Long.MinProbability)
	}
	if current.Short != baseline.Short {
		t.Errorf("Short = %+v, want the untouched baseline %+v", current.Short, baseline.Short)
	}
}
