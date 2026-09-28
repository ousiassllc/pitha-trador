package calibration

import (
	"context"
	"fmt"
	"math"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository"
)

// probabilityEpsilon keeps Log Loss finite when a sample's Confidence is
// exactly 0 or 1 (ln(0) would otherwise be -Inf).
const probabilityEpsilon = 1e-9

// Service composes CalibrationRepository's stored calibration_outcomes
// data with Metrics's aggregation logic (docs/api/endpoints.md §GET
// /api/v1/calibration).
type Service struct {
	outcomes *repository.CalibrationRepository
}

// NewService returns a Service backed by outcomes.
func NewService(outcomes *repository.CalibrationRepository) *Service {
	return &Service{outcomes: outcomes}
}

// Metrics loads every labeled Jev trader decision/horizon outcome and
// aggregates them via the package-level Metrics function (FR-CAL-2/3). It
// matches internal/web/handler.CalibrationSource's signature, so a
// *Service can be passed directly to
// internal/router.WithCalibrationSource.
func (s *Service) Metrics(ctx context.Context) (domain.CalibrationMetrics, error) {
	samples, err := s.outcomes.ListLabeledSamples(ctx)
	if err != nil {
		return domain.CalibrationMetrics{}, fmt.Errorf("calibration: load labeled samples: %w", err)
	}
	return Metrics(samples), nil
}

// Metrics computes Calibration's evaluation metrics (functional.md
// FR-CAL-2, FR-CAL-3) over every labeled Jev trader decision/horizon
// outcome in samples: Brier Score and Log Loss over every sample
// regardless of confidence, plus a Reliability Curve
// (domain.ConfidenceBucket per domain.DefaultConfidenceBucketRanges) and
// the Expected Calibration Error derived from it. A sample's Confidence
// falling outside every bucket range still counts toward
// BrierScore/LogLoss but not toward any bucket or the ECE.
func Metrics(samples []domain.LabeledSample) domain.CalibrationMetrics {
	ranges := domain.DefaultConfidenceBucketRanges
	buckets := make([]domain.ConfidenceBucket, len(ranges))
	bucketConfidenceSum := make([]float64, len(ranges))
	bucketCorrectSum := make([]float64, len(ranges))
	bucketReturnSum := make([]float64, len(ranges))
	for i, r := range ranges {
		buckets[i].Range = r.Range
	}

	var brierSum, logLossSum float64
	for _, sample := range samples {
		outcome := 0.0
		if sample.WasDirectionCorrect {
			outcome = 1.0
		}
		diff := sample.Confidence - outcome
		brierSum += diff * diff

		p := math.Min(math.Max(sample.Confidence, probabilityEpsilon), 1-probabilityEpsilon)
		logLossSum += -(outcome*math.Log(p) + (1-outcome)*math.Log(1-p))

		signedReturn := sample.FutureReturn
		if sample.Direction == domain.JevDirectionShort {
			signedReturn = -signedReturn
		}

		if idx, ok := bucketIndex(ranges, sample.Confidence); ok {
			buckets[idx].SampleCount++
			bucketConfidenceSum[idx] += sample.Confidence
			bucketCorrectSum[idx] += outcome
			bucketReturnSum[idx] += signedReturn
		}
	}

	metrics := domain.CalibrationMetrics{Buckets: buckets, SampleCount: len(samples)}
	if len(samples) > 0 {
		metrics.BrierScore = brierSum / float64(len(samples))
		metrics.LogLoss = logLossSum / float64(len(samples))
	}

	var eceSum float64
	for i := range buckets {
		if buckets[i].SampleCount == 0 {
			continue
		}
		n := float64(buckets[i].SampleCount)
		buckets[i].DirectionAccuracy = bucketCorrectSum[i] / n
		buckets[i].AvgFutureReturnPct = bucketReturnSum[i] / n
		if len(samples) > 0 {
			avgConfidence := bucketConfidenceSum[i] / n
			eceSum += n / float64(len(samples)) * math.Abs(buckets[i].DirectionAccuracy-avgConfidence)
		}
	}
	metrics.ExpectedCalibrationError = eceSum

	return metrics
}

// bucketIndex returns the index of the first range in ranges containing
// confidence ([Low, High), except the last range also includes High
// itself), or (0, false) if confidence falls in none of them.
func bucketIndex(ranges []domain.ConfidenceBucketRange, confidence float64) (int, bool) {
	for i, r := range ranges {
		if confidence >= r.Low && (confidence < r.High || (i == len(ranges)-1 && confidence == r.High)) {
			return i, true
		}
	}
	return 0, false
}
