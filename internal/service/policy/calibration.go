package policy

import (
	"context"
	"fmt"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// CalibrationSource supplies Calibration's per-bucket sample counts
// (internal/service/calibration.Service satisfies it).
type CalibrationSource interface {
	// BucketSampleCount returns how many labeled samples fall in the
	// confidence bucket containing confidence, or 0 when it is in none.
	// It must be cheap: it runs on every directional Jev trader job.
	BucketSampleCount(ctx context.Context, confidence float64) (int, error)
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

	count, err := h.calib.BucketSampleCount(ctx, *d.Confidence)
	if err != nil {
		return false, fmt.Errorf("count calibration samples: %w", err)
	}
	return count >= minSamples, nil
}
