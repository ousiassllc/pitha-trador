// Package metalabelsettings holds the Settings-screen configuration of the
// meta-labeling secondary model (issue #759, parent #752): which model
// algorithm and calibration method it uses, the time of day of the nightly
// retraining, and the minimum expected value (bps) a signal must clear to be
// taken (internal/service/metalabel.DecideConfig.MinEVBps). They live in
// runtime_settings (docs/architecture/er.md §runtime_settings) under Keys;
// there is no yaml or environment source.
package metalabelsettings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"regexp"
	"strings"
	"time"
)

// runtime_settings keys. Model, Calibration and RetrainTime are JSON strings,
// MinEVBps a JSON number.
const (
	KeyModel       = "metalabel.model"
	KeyCalibration = "metalabel.calibration"
	KeyRetrainTime = "metalabel.retrain_time"
	KeyMinEVBps    = "metalabel.min_ev_bps"
)

// Model algorithms (metalabel_models.algorithm).
const (
	ModelLogisticRegression = "logistic_regression"
)

// Calibration methods (internal/service/metalabel.Calibration*).
const (
	CalibrationPlatt    = "platt"
	CalibrationIsotonic = "isotonic"
)

// Shipped defaults.
const (
	DefaultModel       = ModelLogisticRegression
	DefaultCalibration = CalibrationPlatt
	DefaultRetrainTime = "18:00"
	DefaultMinEVBps    = 0.0
)

// MaxMinEVBps is the largest minimum expected value accepted (bps): well above
// any realistic edge on the triple-barrier label widths (barriersettings.MaxBps).
const MaxMinEVBps = 1000.0

// Nightly retraining must not compete with the trading session or run before
// the day's triple-barrier labels exist: the session ends 15:30 and a label
// window is at most 60 minutes (barriersettings.MaxTimeoutMinutes), so the
// retrain time is rejected between RetrainBlockedFrom (inclusive) and
// RetrainBlockedTo (exclusive), JST. Evening and overnight times are allowed.
const (
	RetrainBlockedFrom = "08:00"
	RetrainBlockedTo   = "16:30"
)

// Models and Calibrations are the accepted values of KeyModel and
// KeyCalibration, in display order.
var (
	Models       = []string{ModelLogisticRegression}
	Calibrations = []string{CalibrationPlatt, CalibrationIsotonic}
)

// Settings is the effective meta-labeling configuration.
type Settings struct {
	// Model is one of Models.
	Model string
	// Calibration is one of Calibrations.
	Calibration string
	// RetrainTime is the nightly retraining time of day, "HH:MM" JST.
	RetrainTime string
	// MinEVBps is the minimum calibrated expected value (bps, >= 0) a signal
	// must exceed to be taken. A take always needs EV > 0 even at 0.
	MinEVBps float64
}

// Default returns the shipped settings: logistic regression with Platt
// calibration, retraining at 18:00 and no extra EV margin.
func Default() Settings {
	return Settings{Model: DefaultModel, Calibration: DefaultCalibration, RetrainTime: DefaultRetrainTime, MinEVBps: DefaultMinEVBps}
}

// Keys returns every runtime_settings key Apply accepts.
func Keys() []string { return []string{KeyModel, KeyCalibration, KeyRetrainTime, KeyMinEVBps} }

// IsKey reports whether key is one of Keys.
func IsKey(key string) bool {
	switch key {
	case KeyModel, KeyCalibration, KeyRetrainTime, KeyMinEVBps:
		return true
	}
	return false
}

// IsStringKey reports whether key's value is a JSON string (everything but
// KeyMinEVBps).
func IsStringKey(key string) bool { return IsKey(key) && key != KeyMinEVBps }

// Text returns s's value for key as the Settings screen shows it, or ok=false
// when key is not one of Keys.
func (s Settings) Text(key string) (text string, ok bool) {
	switch key {
	case KeyModel:
		return s.Model, true
	case KeyCalibration:
		return s.Calibration, true
	case KeyRetrainTime:
		return s.RetrainTime, true
	case KeyMinEVBps:
		return fmt.Sprintf("%g", s.MinEVBps), true
	}
	return "", false
}

var retrainTimePattern = regexp.MustCompile(`^\d{1,2}:\d{2}$`)

// Normalize validates the operator's text for a string key and returns the
// canonical value to store: the lower-cased, trimmed model or calibration
// name, or the zero-padded "HH:MM" retrain time. It rejects an unknown name,
// a malformed time, and a time inside the trading-hours window
// [RetrainBlockedFrom, RetrainBlockedTo).
func Normalize(key, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	switch key {
	case KeyModel:
		return oneOf(key, strings.ToLower(raw), Models)
	case KeyCalibration:
		return oneOf(key, strings.ToLower(raw), Calibrations)
	case KeyRetrainTime:
		return normalizeRetrainTime(raw)
	}
	return "", fmt.Errorf("metalabelsettings: %q is not a string meta-labeling setting key", key)
}

func oneOf(key, v string, allowed []string) (string, error) {
	for _, a := range allowed {
		if v == a {
			return v, nil
		}
	}
	return "", fmt.Errorf("%s は %s のいずれかで指定してください", key, strings.Join(allowed, " / "))
}

func normalizeRetrainTime(raw string) (string, error) {
	window := fmt.Sprintf("%s〜%s は立会時間・ラベル確定前のため指定できません", RetrainBlockedFrom, RetrainBlockedTo)
	if !retrainTimePattern.MatchString(raw) {
		return "", fmt.Errorf("%s は HH:MM 形式（JST）で指定してください（%s）", KeyRetrainTime, window)
	}
	t, err := time.Parse("15:04", raw)
	if err != nil {
		return "", fmt.Errorf("%s は HH:MM 形式（JST）で指定してください（%s）", KeyRetrainTime, window)
	}
	v := t.Format("15:04")
	if v >= RetrainBlockedFrom && v < RetrainBlockedTo {
		return "", fmt.Errorf("%s: %s", KeyRetrainTime, window)
	}
	return v, nil
}

// Apply writes jsonValue - a runtime_settings.value: a JSON string for the
// string keys, a JSON number for KeyMinEVBps - into the field of s named by
// key after validating it on its own (Normalize / the EV range). A stored
// string must already be canonical; Apply rejects "Platt" or "9:00", which
// the Settings screen never stores.
func Apply(s *Settings, key, jsonValue string) error {
	if !IsKey(key) {
		return fmt.Errorf("metalabelsettings: %q is not a meta-labeling setting key", key)
	}
	if key == KeyMinEVBps {
		var n float64
		if err := json.Unmarshal([]byte(jsonValue), &n); err != nil {
			return fmt.Errorf("metalabelsettings: decode runtime setting %s=%q as a number: %w", key, jsonValue, err)
		}
		if math.IsNaN(n) || math.IsInf(n, 0) || n < 0 || n > MaxMinEVBps {
			return fmt.Errorf("metalabelsettings: runtime setting %s=%q must be 0 以上 %g 以下", key, jsonValue, MaxMinEVBps)
		}
		s.MinEVBps = n
		return nil
	}
	var v string
	if err := json.Unmarshal([]byte(jsonValue), &v); err != nil {
		return fmt.Errorf("metalabelsettings: decode runtime setting %s=%q as a string: %w", key, jsonValue, err)
	}
	canonical, err := Normalize(key, v)
	if err != nil {
		return fmt.Errorf("metalabelsettings: runtime setting %s=%q: %w", key, jsonValue, err)
	}
	if canonical != v {
		return fmt.Errorf("metalabelsettings: runtime setting %s=%q is not in its canonical form %q", key, jsonValue, canonical)
	}
	switch key {
	case KeyModel:
		s.Model = v
	case KeyCalibration:
		s.Calibration = v
	case KeyRetrainTime:
		s.RetrainTime = v
	}
	return nil
}

// Validate checks every field of s (as a whole value, e.g. one built by hand
// rather than through Apply). Violations are named by runtime_settings key
// and joined.
func (s Settings) Validate() error {
	var errs []error
	if _, err := oneOf(KeyModel, s.Model, Models); err != nil {
		errs = append(errs, err)
	}
	if _, err := oneOf(KeyCalibration, s.Calibration, Calibrations); err != nil {
		errs = append(errs, err)
	}
	if v, err := normalizeRetrainTime(s.RetrainTime); err != nil {
		errs = append(errs, err)
	} else if v != s.RetrainTime {
		errs = append(errs, fmt.Errorf("%s は %q の形式（HH:MM）で指定してください", KeyRetrainTime, v))
	}
	if math.IsNaN(s.MinEVBps) || s.MinEVBps < 0 || s.MinEVBps > MaxMinEVBps {
		errs = append(errs, fmt.Errorf("%s は 0 以上 %g 以下で指定してください", KeyMinEVBps, MaxMinEVBps))
	}
	return errors.Join(errs...)
}

// Store is the runtime_settings read access Load needs;
// *system.RuntimeSettingsRepository implements it.
type Store interface {
	Get(ctx context.Context, key string) (value string, ok bool, err error)
}

// Load is the start-up validation of the stored settings: it returns each
// stored row over its default. A row that no longer parses or validates (the
// Settings screen stores only valid ones, so this is a hand-edited or
// damaged row) is rejected: it is logged and the shipped defaults apply for
// all four settings, never a half-applied set. Only a store read failure is
// an error.
func Load(ctx context.Context, store Store) (Settings, error) {
	s := Default()
	for _, key := range Keys() {
		raw, ok, err := store.Get(ctx, key)
		if err != nil {
			return Default(), fmt.Errorf("metalabelsettings: read %s: %w", key, err)
		}
		if !ok {
			continue
		}
		if err := Apply(&s, key, raw); err != nil {
			slog.Warn("metalabelsettings: ignoring invalid stored meta-labeling settings; using defaults", "error", err)
			return Default(), nil
		}
	}
	if err := s.Validate(); err != nil {
		slog.Warn("metalabelsettings: ignoring invalid stored meta-labeling settings; using defaults", "error", err)
		return Default(), nil
	}
	return s, nil
}
