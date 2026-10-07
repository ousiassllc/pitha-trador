package policy

import (
	"context"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// CalibrationSource answers whether a confidence bucket is populated
// enough (internal/service/calibration.Service satisfies it).
type CalibrationSource interface {
	// BucketHasSamples reports whether the confidence bucket containing
	// confidence holds at least min labeled samples (false when it is in
	// no bucket). It must be cheap and independent of the labeling
	// history's size: it runs on every directional Jev trader job.
	BucketHasSamples(ctx context.Context, confidence float64, min int) (bool, error)
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

// WithClock sets the wall clock Handler judges snapshot staleness against
// (default time.Now; tests pin it).
func WithClock(now func() time.Time) HandlerOption {
	return func(h *Handler) { h.now = now }
}

// WithSnapshotMaxAge sets how old the latest bar may be before HandleJob
// skips the job as stale (default domain.MaxSnapshotAge, the ranking-watch
// value; full-scan mode passes the longer full-scan age, issue #686).
func WithSnapshotMaxAge(d time.Duration) HandlerOption {
	return func(h *Handler) { h.snapshotMaxAge = d }
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

	enough, err := h.calib.BucketHasSamples(ctx, *d.Confidence, minSamples)
	if err != nil {
		return false, fmt.Errorf("count calibration samples: %w", err)
	}
	return enough, nil
}
