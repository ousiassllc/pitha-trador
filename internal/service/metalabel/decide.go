package metalabel

import (
	"errors"
	"fmt"
	"math"

	repo "github.com/ousiassllc/pitha-trador/internal/repository/metalabel"
)

// Reasons recorded in Decision.Reason for a skip. A take has an empty Reason.
const (
	ReasonNoModel        = "no_active_model"
	ReasonModelUnusable  = "model_unusable"
	ReasonNoCandidate    = "no_candidate"
	ReasonMissingFeature = "missing_feature"
	ReasonInvalidConfig  = "invalid_config"
	ReasonPredictFailed  = "predict_failed"
	ReasonEVBelowMin     = "ev_not_above_minimum"
)

// DefaultKellyFraction is the share of the full Kelly bet DecideConfig sizes a
// take with: full Kelly is too aggressive for noisy estimates. The resulting
// Decision.SizeFraction is record-only and never sizes an order (issue #781).
const DefaultKellyFraction = 0.25

// DecideConfig is the secondary model's decision policy.
type DecideConfig struct {
	// MinEVBps is the expected value (bps, after costs) a signal must exceed
	// to be taken, >= 0. A take always needs EV > 0 even when this is 0.
	MinEVBps float64
	// KellyFraction scales the Kelly bet size EV/WinBps, in (0, 1]. It only
	// affects the recorded Decision.SizeFraction, not the order quantity.
	KellyFraction float64
}

// DefaultDecideConfig returns MinEVBps 0 and DefaultKellyFraction.
func DefaultDecideConfig() DecideConfig {
	return DecideConfig{MinEVBps: 0, KellyFraction: DefaultKellyFraction}
}

// Validate reports a configuration Decide would not accept.
func (c DecideConfig) Validate() error {
	if !finite(c.MinEVBps) || c.MinEVBps < 0 {
		return fmt.Errorf("metalabel: min EV %v must be a finite number >= 0 bps", c.MinEVBps)
	}
	if !(c.KellyFraction > 0 && c.KellyFraction <= 1) {
		return fmt.Errorf("metalabel: kelly fraction %v must be in (0, 1]", c.KellyFraction)
	}
	return nil
}

// Decision is the secondary model's take/skip verdict for one primary signal,
// shaped like a metalabel_predictions row.
type Decision struct {
	// Action is repo.ActionTake or repo.ActionSkip.
	Action string
	// SizeFraction is the Kelly-based size share: 0 for a skip, in (0, 1] for
	// a take. It is record-only (metalabel_predictions.size_fraction and the
	// logs) and is NOT used for the order quantity, which the Risk limits
	// alone decide (FR-META-3, FR-ENTRY-3). Fixed until the probability
	// calibration is validated on Paper results (issue #781).
	SizeFraction float64
	// RawProbability, CalibratedProbability and ExpectedValueBps are Predict's
	// values; all zero when the signal was skipped before scoring.
	RawProbability        float64
	CalibratedProbability float64
	ExpectedValueBps      float64
	// Reason is why the signal was skipped; empty for a take.
	Reason string
	// FeaturesJSON is the raw feature object for metalabel_predictions.features_json.
	FeaturesJSON string
}

// Decide runs the secondary model on a primary signal: direction is the
// candidate side Jev chose (LONG/SHORT) and f the features at that moment.
// It takes the signal only when the calibrated expected value is above
// cfg.MinEVBps and above zero; the recorded size is the Kelly bet EV/WinBps
// scaled by cfg.KellyFraction and capped at 1 (record-only, see SizeFraction).
//
// Every doubtful case is a skip: no model (m nil, the LatestActiveModel
// not-found case), a model that cannot score, no LONG/SHORT candidate, a
// missing feature, an invalid config. Decide never returns an error: a skip
// is the safe answer, and its Reason says why.
func Decide(m *Model, direction string, f Features, cfg DecideConfig) Decision {
	d := Decision{Action: repo.ActionSkip, FeaturesJSON: f.JSON()}
	if m == nil {
		d.Reason = ReasonNoModel
		return d
	}
	if err := cfg.Validate(); err != nil {
		d.Reason = ReasonInvalidConfig
		return d
	}
	vector, err := f.Vector(direction)
	if err != nil {
		switch {
		case errors.Is(err, ErrNoCandidate):
			d.Reason = ReasonNoCandidate
		default:
			d.Reason = ReasonMissingFeature
		}
		return d
	}
	if m.validate() != nil {
		d.Reason = ReasonModelUnusable
		return d
	}
	p, err := m.Predict(vector)
	if err != nil {
		d.Reason = ReasonPredictFailed
		return d
	}
	d.RawProbability, d.CalibratedProbability, d.ExpectedValueBps = p.Raw, p.Calibrated, p.ExpectedValueBps

	if !(p.ExpectedValueBps > 0 && p.ExpectedValueBps > cfg.MinEVBps) {
		d.Reason = ReasonEVBelowMin
		return d
	}
	size := math.Min(1, cfg.KellyFraction*p.ExpectedValueBps/m.WinBps)
	if !(size > 0) {
		d.Reason = ReasonEVBelowMin
		return d
	}
	d.Action, d.SizeFraction = repo.ActionTake, size
	return d
}
