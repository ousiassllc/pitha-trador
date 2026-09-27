package assist_test

import (
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/assist"
)

func healthyThresholds() config.PolicyDirectionThresholds {
	return config.PolicyDirectionThresholds{
		MinProbability:             0.60,
		MinEntryQuality:            domain.JevEntryQualityGood,
		MinContinuationProbability: 0.55,
		MaxToxicFlow:               0.40,
		MaxLiquidityStressed:       0.40,
	}
}

func TestSol_Analyze_NoProposalWhenCalibrationHealthy(t *testing.T) {
	sol := assist.NewSol()
	healthyCalibration := domain.CalibrationMetrics{
		Buckets: []domain.ConfidenceBucket{
			{Range: "0.60-0.70", DirectionAccuracy: 0.70, AvgFutureReturnPct: 0.3, SampleCount: 50},
		},
		ExpectedCalibrationError: 0.03,
		SampleCount:              50,
	}
	in := assist.SolAnalysisInput{
		Long:  assist.DirectionCalibration{Thresholds: healthyThresholds(), Calibration: healthyCalibration},
		Short: assist.DirectionCalibration{Thresholds: healthyThresholds(), Calibration: healthyCalibration},
	}

	proposal, ok, err := sol.Analyze(in)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if ok {
		t.Fatalf("Analyze() ok = true, proposal = %+v, want no proposal when calibration is healthy", proposal)
	}
}

func TestSol_Analyze_ProposesRaisingMinProbabilityForWeakBucket(t *testing.T) {
	sol := assist.NewSol()
	weakLongCalibration := domain.CalibrationMetrics{
		Buckets: []domain.ConfidenceBucket{
			// MinProbability=0.60 falls in this range; low accuracy and
			// negative PnL with enough samples to act on.
			{Range: "0.60-0.70", DirectionAccuracy: 0.45, AvgFutureReturnPct: -0.2, SampleCount: 30},
		},
		ExpectedCalibrationError: 0.03,
		SampleCount:              30,
	}
	healthyShort := domain.CalibrationMetrics{
		Buckets: []domain.ConfidenceBucket{
			{Range: "0.60-0.70", DirectionAccuracy: 0.70, AvgFutureReturnPct: 0.3, SampleCount: 50},
		},
		ExpectedCalibrationError: 0.03,
		SampleCount:              50,
	}
	in := assist.SolAnalysisInput{
		Long:  assist.DirectionCalibration{Thresholds: healthyThresholds(), Calibration: weakLongCalibration},
		Short: assist.DirectionCalibration{Thresholds: healthyThresholds(), Calibration: healthyShort},
	}

	proposal, ok, err := sol.Analyze(in)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if !ok {
		t.Fatalf("Analyze() ok = false, want a proposal for the weak LONG bucket")
	}

	changes, err := domain.ParsePolicyChanges(proposal.ProposedChangesJSON)
	if err != nil {
		t.Fatalf("ParsePolicyChanges: %v", err)
	}
	if len(changes) != 1 {
		t.Fatalf("ParsePolicyChanges() = %+v, want exactly one change (long min_probability)", changes)
	}
	if changes[0].Key != domain.PolicyKeyLongMinProbability {
		t.Fatalf("changes[0].Key = %q, want %q", changes[0].Key, domain.PolicyKeyLongMinProbability)
	}
	if err := domain.ValidatePolicyChanges(changes); err != nil {
		t.Fatalf("ValidatePolicyChanges(sol's own proposal) = %v, want nil (Sol must never propose an invalid change)", err)
	}
}

func TestSol_Analyze_ProposesRaisingMinEntryQualityForPoorCalibration(t *testing.T) {
	sol := assist.NewSol()
	poorlyCalibrated := domain.CalibrationMetrics{
		Buckets: []domain.ConfidenceBucket{
			{Range: "0.60-0.70", DirectionAccuracy: 0.70, AvgFutureReturnPct: 0.3, SampleCount: 50},
		},
		ExpectedCalibrationError: 0.25, // above targetExpectedCalibrationError
		SampleCount:              50,
	}
	healthy := domain.CalibrationMetrics{
		Buckets: []domain.ConfidenceBucket{
			{Range: "0.60-0.70", DirectionAccuracy: 0.70, AvgFutureReturnPct: 0.3, SampleCount: 50},
		},
		ExpectedCalibrationError: 0.03,
		SampleCount:              50,
	}
	in := assist.SolAnalysisInput{
		Long:  assist.DirectionCalibration{Thresholds: healthyThresholds(), Calibration: poorlyCalibrated},
		Short: assist.DirectionCalibration{Thresholds: healthyThresholds(), Calibration: healthy},
	}

	proposal, ok, err := sol.Analyze(in)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if !ok {
		t.Fatalf("Analyze() ok = false, want a proposal for poor overall calibration (high ECE)")
	}
	changes, err := domain.ParsePolicyChanges(proposal.ProposedChangesJSON)
	if err != nil {
		t.Fatalf("ParsePolicyChanges: %v", err)
	}
	if len(changes) != 1 || changes[0].Key != domain.PolicyKeyLongMinEntryQuality {
		t.Fatalf("changes = %+v, want exactly one long min_entry_quality change", changes)
	}
	if changes[0].OldValue != `"good"` || changes[0].NewValue != `"strong"` {
		t.Fatalf("changes[0] = %+v, want good->strong (one rank step)", changes[0])
	}
}

func TestSol_Analyze_IgnoresWeakBucketBelowMinimumSampleCount(t *testing.T) {
	sol := assist.NewSol()
	tooFewSamples := domain.CalibrationMetrics{
		Buckets: []domain.ConfidenceBucket{
			{Range: "0.60-0.70", DirectionAccuracy: 0.20, AvgFutureReturnPct: -0.9, SampleCount: 3},
		},
		ExpectedCalibrationError: 0.03,
		SampleCount:              3,
	}
	in := assist.SolAnalysisInput{
		Long:  assist.DirectionCalibration{Thresholds: healthyThresholds(), Calibration: tooFewSamples},
		Short: assist.DirectionCalibration{Thresholds: healthyThresholds(), Calibration: tooFewSamples},
	}

	_, ok, err := sol.Analyze(in)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if ok {
		t.Fatalf("Analyze() ok = true, want no proposal when the weak bucket's sample count is too small to act on")
	}
}
