package policy

import (
	"context"
	"fmt"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// CalibrationSource supplies Calibration's aggregated outcomes
// (internal/service/calibration.Service satisfies it).
type CalibrationSource interface {
	Metrics(ctx context.Context) (domain.CalibrationMetrics, error)
}

// HandlerOption configures optional Handler behavior.
type HandlerOption func(*Handler)

// WithCalibration makes Handler evaluate FR-POLICY-3's "キャリブレーション
// 対象外" against src: a directional decision whose confidence bucket
// holds fewer than Thresholds.Policy.MinCalibrationSamples labeled
// samples is not calibrated and becomes NONE.
func WithCalibration(src CalibrationSource) HandlerOption {
	return func(h *Handler) { h.calib = src }
}

// calibrated reports whether decision's confidence bucket has enough
// labeled Calibration samples to trust (Input.Calibrated). Always true
// without a CalibrationSource, a zero MinCalibrationSamples, or a NONE
// decision (nothing to calibrate); false for a directional decision with
// no confidence or one outside every bucket.
func (h *Handler) calibrated(ctx context.Context, d domain.JevDecision) (bool, error) {
	minSamples := h.engine.thresholds.Policy.MinCalibrationSamples
	if h.calib == nil || minSamples <= 0 || d.Direction == nil || *d.Direction == domain.JevDirectionNone {
		return true, nil
	}
	if d.Confidence == nil {
		return false, nil
	}

	metrics, err := h.calib.Metrics(ctx)
	if err != nil {
		return false, fmt.Errorf("load calibration metrics: %w", err)
	}
	for i, r := range domain.DefaultConfidenceBucketRanges {
		last := i == len(domain.DefaultConfidenceBucketRanges)-1
		c := *d.Confidence
		if c >= r.Low && (c < r.High || (last && c == r.High)) {
			return i < len(metrics.Buckets) && metrics.Buckets[i].SampleCount >= minSamples, nil
		}
	}
	return false, nil
}
