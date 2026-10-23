package policy

import (
	"context"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/policy/calibrationgate"
)

// CalibrationSource answers whether a confidence bucket is populated enough, over
// the live Trader prompt version's samples (internal/service/calibration.Service
// satisfies it; issue #881, see calibrationgate).
type CalibrationSource = calibrationgate.Source

// HandlerOption configures optional Handler behavior.
type HandlerOption func(*Handler)

// WithCalibration makes Handler evaluate FR-POLICY-3's "キャリブレーション
// 対象外" against src: a directional decision whose confidence bucket
// holds fewer than Thresholds.Policy.MinCalibrationSamples labeled
// samples is not calibrated and becomes NONE.
func WithCalibration(src CalibrationSource) HandlerOption {
	return func(h *Handler) { h.calib.Source = src }
}

// WithCalibrationScope sets what the calibration check is judged for
// (calibrationgate.Gate): the operating mode (metalabelsettings.RuntimeMode), the
// live version (read to name it in the reasons), and allVersions, the
// count over every Trader version. Only Paper may fall back to allVersions, and
// the decision then says so; any other mode, and the default of none, never
// does. A nil allVersions is no fallback.
func WithCalibrationScope(mode metalabelsettings.Mode, live func(context.Context) (string, error), allVersions CalibrationSource) HandlerOption {
	return func(h *Handler) { h.calib.Mode, h.calib.Live, h.calib.AllVersions = mode, live, allVersions }
}

// WithClock sets the wall clock Handler judges snapshot staleness against
// (default time.Now; tests pin it).
func WithClock(now func() time.Time) HandlerOption {
	return func(h *Handler) { h.now = now }
}

// WithSnapshotMaxAge sets how old the latest bar may be before HandleJob
// skips the job as stale (default domain.DefaultMaxSnapshotAge, the ranking-watch
// value; full-scan mode passes the longer full-scan age, issue #686).
func WithSnapshotMaxAge(d time.Duration) HandlerOption {
	return func(h *Handler) { h.snapshotMaxAge = d }
}

// calibrated reports whether decision's confidence bucket has enough
// labeled Calibration samples to trust (Input.Calibrated), and the detail when
// the live version's own were too few (Input.CalibrationDetail). Always true
// without a CalibrationSource, a zero MinCalibrationSamples, or a NONE
// decision (nothing to calibrate); false for a directional decision with
// no confidence or one outside every bucket.
func (h *Handler) calibrated(ctx context.Context, d domain.JevDecision) (calibrationgate.Verdict, error) {
	minSamples := h.engine.thresholds.Policy.MinCalibrationSamples
	if h.calib.Source == nil || minSamples <= 0 || d.Direction == nil || *d.Direction == domain.JevDirectionNone {
		return calibrationgate.Verdict{OK: true}, nil
	}
	if d.Confidence == nil {
		return calibrationgate.Verdict{}, nil
	}
	return h.calib.Check(ctx, *d.Confidence, minSamples)
}
