package calibration

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
)

// probabilityEpsilon keeps Log Loss finite when a sample's Confidence is
// exactly 0 or 1 (ln(0) would otherwise be -Inf).
const probabilityEpsilon = 1e-9

// Service composes CalibrationRepository's stored calibration_outcomes
// data with Metrics's aggregation logic (docs/api/endpoints.md §GET
// /api/v1/calibration).
type Service struct {
	outcomes *judgement.CalibrationRepository
	trades   TradeSource
}

// TradeSource lists the closed positions traced back to the Jev trader
// decision that opened them (*decisiontrade.Repository).
type TradeSource interface {
	List(ctx context.Context) ([]domain.DecisionTrade, error)
}

// NewService returns a Service backed by outcomes and trades.
func NewService(outcomes *judgement.CalibrationRepository, trades TradeSource) *Service {
	return &Service{outcomes: outcomes, trades: trades}
}

// Metrics loads every labeled Jev trader decision/horizon outcome and
// aggregates them via the package-level Metrics function, then adds each
// confidence bucket's realized PnL from the closed positions those
// decisions opened (WithTradePnL, FR-CAL-2/3). It
// matches internal/web/handler.CalibrationSource's signature, so a
// *Service can be passed directly to
// internal/router.WithCalibrationSource.
func (s *Service) Metrics(ctx context.Context) (domain.CalibrationMetrics, error) {
	samples, err := s.outcomes.ListLabeledSamples(ctx)
	if err != nil {
		return domain.CalibrationMetrics{}, fmt.Errorf("calibration: load labeled samples: %w", err)
	}
	trades, err := s.trades.List(ctx)
	if err != nil {
		return domain.CalibrationMetrics{}, fmt.Errorf("calibration: load decision trades: %w", err)
	}
	return WithTradePnL(Metrics(samples), trades), nil
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
	dirCorrect := map[string]float64{}
	dirReturn := map[string]float64{}
	dirCount := map[string]int{}
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

		dirCount[sample.Direction]++
		dirCorrect[sample.Direction] += outcome
		dirReturn[sample.Direction] += signedReturn

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
		buckets[i].AvgConfidence = bucketConfidenceSum[i] / n
		eceSum += n / float64(len(samples)) * math.Abs(buckets[i].DirectionAccuracy-buckets[i].AvgConfidence)
	}
	metrics.ExpectedCalibrationError = eceSum

	for _, direction := range []string{domain.JevDirectionLong, domain.JevDirectionShort} {
		dm := domain.DirectionMetric{Direction: direction, SampleCount: dirCount[direction]}
		if dm.SampleCount > 0 {
			n := float64(dm.SampleCount)
			dm.DirectionAccuracy = dirCorrect[direction] / n
			dm.AvgFutureReturnPct = dirReturn[direction] / n
		}
		metrics.ByDirection = append(metrics.ByDirection, dm)
	}

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

// DirectionMetricsSince aggregates the labeled outcomes of Jev trader
// decisions timestamped at or after since into separate LONG and SHORT
// CalibrationMetrics - the per-direction input Sol's daily analysis
// (internal/service/selfimprove, FR-SELFIMPROVE-1) weighs against each
// direction's own policy.* thresholds.
func (s *Service) DirectionMetricsSince(ctx context.Context, since time.Time) (long, short domain.CalibrationMetrics, err error) {
	samples, err := s.outcomes.ListLabeledSamplesSince(ctx, since)
	if err != nil {
		return domain.CalibrationMetrics{}, domain.CalibrationMetrics{}, fmt.Errorf("calibration: load labeled samples since %s: %w", since, err)
	}
	var longSamples, shortSamples []domain.LabeledSample
	for _, sample := range samples {
		switch sample.Direction {
		case domain.JevDirectionLong:
			longSamples = append(longSamples, sample)
		case domain.JevDirectionShort:
			shortSamples = append(shortSamples, sample)
		}
	}
	return Metrics(longSamples), Metrics(shortSamples), nil
}
