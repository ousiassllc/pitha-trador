package calibration_test

import (
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/calibration"
)

func TestMetrics_EmptySamples(t *testing.T) {
	got := calibration.Metrics(nil)
	if got.SampleCount != 0 || got.BrierScore != 0 || got.LogLoss != 0 || got.ExpectedCalibrationError != 0 {
		t.Fatalf("Metrics(nil) = %+v, want all-zero", got)
	}
	if len(got.Buckets) != len(domain.DefaultConfidenceBucketRanges) {
		t.Fatalf("Metrics(nil).Buckets has %d entries, want %d (one per DefaultConfidenceBucketRanges, even with no samples)",
			len(got.Buckets), len(domain.DefaultConfidenceBucketRanges))
	}
	for i, b := range got.Buckets {
		if b.SampleCount != 0 || b.Range != domain.DefaultConfidenceBucketRanges[i].Range {
			t.Fatalf("Metrics(nil).Buckets[%d] = %+v, want SampleCount=0 and Range=%q", i, b, domain.DefaultConfidenceBucketRanges[i].Range)
		}
	}
}

func TestMetrics_BrierScoreAndLogLoss(t *testing.T) {
	samples := []domain.LabeledSample{
		{Direction: domain.JevDirectionLong, Confidence: 0.8, FutureReturn: 1, WasDirectionCorrect: true},
		{Direction: domain.JevDirectionLong, Confidence: 0.8, FutureReturn: -1, WasDirectionCorrect: false},
	}
	got := calibration.Metrics(samples)

	// Brier: mean((0.8-1)^2, (0.8-0)^2) = mean(0.04, 0.64) = 0.34
	if !almostEqual(got.BrierScore, 0.34) {
		t.Fatalf("BrierScore = %v, want ~0.34", got.BrierScore)
	}
	if got.SampleCount != 2 {
		t.Fatalf("SampleCount = %d, want 2", got.SampleCount)
	}
	if got.LogLoss <= 0 {
		t.Fatalf("LogLoss = %v, want > 0", got.LogLoss)
	}
}

func TestMetrics_BucketDirectionAccuracyAndAvgReturn(t *testing.T) {
	samples := []domain.LabeledSample{
		// bucket 0.80-0.90: two LONG samples, one correct, one not.
		{Direction: domain.JevDirectionLong, Confidence: 0.85, FutureReturn: 2.0, WasDirectionCorrect: true},
		{Direction: domain.JevDirectionLong, Confidence: 0.85, FutureReturn: -1.0, WasDirectionCorrect: false},
		// bucket 0.90-1.00: one SHORT sample, correct (price fell).
		{Direction: domain.JevDirectionShort, Confidence: 0.95, FutureReturn: -3.0, WasDirectionCorrect: true},
	}
	got := calibration.Metrics(samples)

	var b1, b2 domain.ConfidenceBucket
	for _, b := range got.Buckets {
		switch b.Range {
		case "0.80-0.90":
			b1 = b
		case "0.90-1.00":
			b2 = b
		}
	}

	if b1.SampleCount != 2 {
		t.Fatalf("bucket 0.80-0.90 SampleCount = %d, want 2", b1.SampleCount)
	}
	if !almostEqual(b1.DirectionAccuracy, 0.5) {
		t.Fatalf("bucket 0.80-0.90 DirectionAccuracy = %v, want 0.5", b1.DirectionAccuracy)
	}
	// direction-adjusted avg return for LONG samples: mean(2.0, -1.0) = 0.5
	if !almostEqual(b1.AvgFutureReturnPct, 0.5) {
		t.Fatalf("bucket 0.80-0.90 AvgFutureReturnPct = %v, want 0.5", b1.AvgFutureReturnPct)
	}

	if b2.SampleCount != 1 {
		t.Fatalf("bucket 0.90-1.00 SampleCount = %d, want 1", b2.SampleCount)
	}
	if !almostEqual(b2.DirectionAccuracy, 1.0) {
		t.Fatalf("bucket 0.90-1.00 DirectionAccuracy = %v, want 1.0", b2.DirectionAccuracy)
	}
	// direction-adjusted avg return for a SHORT sample: -(-3.0) = 3.0
	if !almostEqual(b2.AvgFutureReturnPct, 3.0) {
		t.Fatalf("bucket 0.90-1.00 AvgFutureReturnPct = %v, want 3.0", b2.AvgFutureReturnPct)
	}
}

func TestMetrics_ConfidenceOutsideEveryBucketStillCountsTowardBrierButNotBuckets(t *testing.T) {
	samples := []domain.LabeledSample{
		{Direction: domain.JevDirectionLong, Confidence: 0.30, FutureReturn: 1.0, WasDirectionCorrect: true},
	}
	got := calibration.Metrics(samples)
	if got.SampleCount != 1 {
		t.Fatalf("SampleCount = %d, want 1", got.SampleCount)
	}
	for _, b := range got.Buckets {
		if b.SampleCount != 0 {
			t.Fatalf("bucket %q SampleCount = %d, want 0 (confidence 0.30 falls in no bucket)", b.Range, b.SampleCount)
		}
	}
	if !almostEqual(got.BrierScore, (0.30-1.0)*(0.30-1.0)) {
		t.Fatalf("BrierScore = %v, want the single out-of-bucket sample to still count", got.BrierScore)
	}
}

func TestMetrics_PerfectCalibrationHasZeroECE(t *testing.T) {
	// Every sample's confidence exactly matches its bucket's observed
	// accuracy: ECE should be 0.
	samples := []domain.LabeledSample{
		{Direction: domain.JevDirectionLong, Confidence: 0.90, FutureReturn: 1, WasDirectionCorrect: true},
		{Direction: domain.JevDirectionLong, Confidence: 0.90, FutureReturn: 1, WasDirectionCorrect: true},
		{Direction: domain.JevDirectionLong, Confidence: 0.90, FutureReturn: 1, WasDirectionCorrect: true},
		{Direction: domain.JevDirectionLong, Confidence: 0.90, FutureReturn: 1, WasDirectionCorrect: true},
		{Direction: domain.JevDirectionLong, Confidence: 0.90, FutureReturn: -1, WasDirectionCorrect: false},
	}
	got := calibration.Metrics(samples)
	if !almostEqual(got.ExpectedCalibrationError, 0.1) {
		t.Fatalf("ExpectedCalibrationError = %v, want ~0.1 (|accuracy 0.8 - confidence 0.9| * weight 1.0)", got.ExpectedCalibrationError)
	}
}
