// Package regimesettings holds the Settings-screen parameters of the market
// regime detection and the momentum / VWAP-reversion switch (issue #755,
// parent #751): the window the market index's 1-minute returns are measured
// over, the autocorrelation and volatility thresholds that classify the
// regime, the VWAP-deviation z-score a reversion entry needs, and the on/off
// switch of the paper A/B comparison. They live in runtime_settings
// (docs/architecture/er.md §runtime_settings) under Keys; there is no yaml or
// environment source.
package regimesettings

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain/marketregime"
)

// runtime_settings keys. The window and the A/B switch are JSON integers
// (minutes; 0 or 1), the others JSON numbers.
const (
	KeyWindowMinutes     = "regime.window_minutes"
	KeyAutocorrThreshold = "regime.autocorr_threshold"
	KeyMinVolatilityBps  = "regime.min_volatility_bps"
	KeyVWAPZThreshold    = "regime.vwap_z_threshold"
	KeyABEnabled         = "regime.ab_enabled"
)

// Accepted ranges.
const (
	// MinWindowMinutes and MaxWindowMinutes bound the index-return window.
	// The lower bound keeps the autocorrelation from resting on a handful of
	// returns; the upper bound is what the snapshot history can supply
	// (featureengine.HistoryLookbackBars covers 35 bars, the regime reads its
	// own, longer history).
	MinWindowMinutes = 5
	MaxWindowMinutes = 120
	// MaxBps is the highest volatility floor accepted (bps).
	MaxBps = 1000.0
	// MaxVWAPZ is the highest VWAP-deviation z-score accepted.
	MaxVWAPZ = 10.0
)

// Shipped defaults. The A/B switch is off: until an operator enables it,
// every decision uses the existing momentum entry.
const (
	defaultWindowMinutes     = 30
	defaultAutocorrThreshold = 0.2
	defaultMinVolatilityBps  = 0.5
)

// Settings is the effective regime configuration.
type Settings struct {
	// Window is how far back the index's 1-minute returns are taken.
	Window time.Duration
	// AutocorrThreshold is the |lag-1 autocorrelation| from which the market
	// counts as trending (positive) or mean-reverting (negative).
	AutocorrThreshold float64
	// MinVolatilityBps is the standard deviation of the 1-minute returns (bps)
	// below which the market is too quiet to classify (RegimeUnknown).
	MinVolatilityBps float64
	// VWAPZThreshold is the |z| a VWAP-reversion entry needs.
	VWAPZThreshold float64
	// ABEnabled splits decisions between the momentum arm and the regime
	// arm; off, every decision is the momentum arm.
	ABEnabled bool
}

// Default returns the shipped settings: a 30-minute window, a 0.2
// autocorrelation threshold, a 0.5 bps volatility floor, a VWAP z of 2 and
// the A/B comparison off.
func Default() Settings {
	return Settings{
		Window:            defaultWindowMinutes * time.Minute,
		AutocorrThreshold: defaultAutocorrThreshold,
		MinVolatilityBps:  defaultMinVolatilityBps,
		VWAPZThreshold:    marketregime.DefaultVWAPZThreshold,
	}
}

// Keys returns every runtime_settings key Apply accepts.
func Keys() []string {
	return []string{KeyWindowMinutes, KeyAutocorrThreshold, KeyMinVolatilityBps, KeyVWAPZThreshold, KeyABEnabled}
}

// IsKey reports whether key is one of Keys.
func IsKey(key string) bool {
	switch key {
	case KeyWindowMinutes, KeyAutocorrThreshold, KeyMinVolatilityBps, KeyVWAPZThreshold, KeyABEnabled:
		return true
	}
	return false
}

// IsIntegerKey reports whether key's value is a whole number (the window in
// minutes and the 0/1 switch) rather than a decimal.
func IsIntegerKey(key string) bool { return key == KeyWindowMinutes || key == KeyABEnabled }

// WindowMinutes is Window in whole minutes.
func (s Settings) WindowMinutes() int { return int(s.Window / time.Minute) }

// Text returns s's value for key as the Settings screen shows it, or ok=false
// when key is not one of Keys.
func (s Settings) Text(key string) (text string, ok bool) {
	switch key {
	case KeyWindowMinutes:
		return strconv.Itoa(s.WindowMinutes()), true
	case KeyAutocorrThreshold:
		return formatNumber(s.AutocorrThreshold), true
	case KeyMinVolatilityBps:
		return formatNumber(s.MinVolatilityBps), true
	case KeyVWAPZThreshold:
		return formatNumber(s.VWAPZThreshold), true
	case KeyABEnabled:
		if s.ABEnabled {
			return "1", true
		}
		return "0", true
	}
	return "", false
}

func formatNumber(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

// Apply writes jsonValue - a runtime_settings.value, a JSON number - into the
// field of s named by key after checking its range. Every field stands alone,
// so there is no cross-field validation.
func Apply(s *Settings, key, jsonValue string) error {
	if !IsKey(key) {
		return fmt.Errorf("regimesettings: %q is not a regime setting key", key)
	}
	var n float64
	if err := json.Unmarshal([]byte(jsonValue), &n); err != nil {
		return fmt.Errorf("regimesettings: decode runtime setting %s=%q as a number: %w", key, jsonValue, err)
	}
	if math.IsNaN(n) || math.IsInf(n, 0) {
		return fmt.Errorf("regimesettings: runtime setting %s=%q is not finite", key, jsonValue)
	}
	switch key {
	case KeyWindowMinutes:
		if n != math.Trunc(n) || n < MinWindowMinutes || n > MaxWindowMinutes {
			return rangeError(key, jsonValue, "%d〜%d の整数", MinWindowMinutes, MaxWindowMinutes)
		}
		s.Window = time.Duration(n) * time.Minute
	case KeyAutocorrThreshold:
		if n <= 0 || n >= 1 {
			return rangeError(key, jsonValue, "0 より大きく 1 より小さい")
		}
		s.AutocorrThreshold = n
	case KeyMinVolatilityBps:
		if n < 0 || n > MaxBps {
			return rangeError(key, jsonValue, "0 以上 %g 以下", MaxBps)
		}
		s.MinVolatilityBps = n
	case KeyVWAPZThreshold:
		if n <= 0 || n > MaxVWAPZ {
			return rangeError(key, jsonValue, "0 より大きく %g 以下", MaxVWAPZ)
		}
		s.VWAPZThreshold = n
	case KeyABEnabled:
		if n != 0 && n != 1 {
			return rangeError(key, jsonValue, "0（無効）または 1（有効）")
		}
		s.ABEnabled = n == 1
	}
	return nil
}

func rangeError(key, jsonValue, format string, args ...any) error {
	return fmt.Errorf("regimesettings: runtime setting %s=%q must be "+format, append([]any{key, jsonValue}, args...)...)
}

// Store is the runtime_settings read access Load needs;
// *system.RuntimeSettingsRepository implements it.
type Store interface {
	Get(ctx context.Context, key string) (value string, ok bool, err error)
}

// Load returns the effective settings: each stored row over its default. A
// stored row that no longer parses (the Settings screen stores only valid
// ones) is logged and leaves that field at its default, so a damaged row
// cannot stop the pipeline; only a store read failure is an error.
func Load(ctx context.Context, store Store) (Settings, error) {
	s := Default()
	for _, key := range Keys() {
		raw, ok, err := store.Get(ctx, key)
		if err != nil {
			return Default(), fmt.Errorf("regimesettings: read %s: %w", key, err)
		}
		if !ok {
			continue
		}
		if err := Apply(&s, key, raw); err != nil {
			slog.Warn("regimesettings: ignoring invalid stored regime setting; using its default", "error", err)
		}
	}
	return s, nil
}
