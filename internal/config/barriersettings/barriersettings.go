// Package barriersettings holds the Settings-screen configuration of the
// triple-barrier calibration label (issue #760, parent #751): the take-profit
// and stop-loss widths (bps), the timeout (minutes) and the assumed trading
// cost (bps) that internal/service/calibration.Labeler measures each Jev
// trader decision against. They live in runtime_settings
// (docs/architecture/er.md §runtime_settings) under Keys; there is no yaml or
// environment source.
package barriersettings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"time"
)

// runtime_settings keys. TP/SL/cost values are JSON numbers in bps, the
// timeout is a JSON integer number of minutes.
const (
	KeyTakeProfitBps  = "calibration.barrier_tp_bps"
	KeyStopLossBps    = "calibration.barrier_sl_bps"
	KeyTimeoutMinutes = "calibration.barrier_timeout_minutes"
	KeyCostBps        = "calibration.barrier_cost_bps"
)

// Accepted ranges.
const (
	// MaxBps is the widest TP/SL/cost accepted (bps).
	MaxBps = 1000.0
	// MinTimeoutMinutes and MaxTimeoutMinutes bound the timeout: a decision's
	// label window must stay inside one trading session (lunch break, close).
	MinTimeoutMinutes = 1
	MaxTimeoutMinutes = 60
)

// Shipped defaults.
const (
	defaultTakeProfitBps = 30.0
	defaultStopLossBps   = 30.0
	defaultTimeoutMins   = 10
	defaultCostBps       = 10.0
)

// Barriers is the effective triple-barrier configuration. TP and SL are the
// widths of the profit and loss barriers as net returns, i.e. after Cost is
// deducted from the gross price move: the take-profit barrier is touched at a
// gross move of TakeProfitBps+CostBps in the trade's favour, the stop-loss
// barrier at a gross move of StopLossBps-CostBps against it (hence Validate
// requires CostBps < StopLossBps). Timeout ends the label window.
type Barriers struct {
	TakeProfitBps float64
	StopLossBps   float64
	Timeout       time.Duration
	// CostBps is the assumed round-trip trading cost: fees + spread +
	// slippage, in bps of the entry price.
	CostBps float64
}

// Default returns the shipped barriers: TP 30 bps, SL 30 bps, timeout 10
// minutes, cost 10 bps.
func Default() Barriers {
	return Barriers{
		TakeProfitBps: defaultTakeProfitBps,
		StopLossBps:   defaultStopLossBps,
		Timeout:       defaultTimeoutMins * time.Minute,
		CostBps:       defaultCostBps,
	}
}

// Keys returns every runtime_settings key Apply accepts.
func Keys() []string {
	return []string{KeyTakeProfitBps, KeyStopLossBps, KeyTimeoutMinutes, KeyCostBps}
}

// IsKey reports whether key is one of Keys.
func IsKey(key string) bool {
	switch key {
	case KeyTakeProfitBps, KeyStopLossBps, KeyTimeoutMinutes, KeyCostBps:
		return true
	}
	return false
}

// IsIntegerKey reports whether key's value is a whole number (the timeout in
// minutes) rather than a fractional bps.
func IsIntegerKey(key string) bool { return key == KeyTimeoutMinutes }

// TimeoutMinutes is Timeout in whole minutes (the labeling horizon).
func (b Barriers) TimeoutMinutes() int { return int(b.Timeout / time.Minute) }

// Text returns b's value for key as the Settings screen shows it, or
// ok=false when key is not one of Keys.
func (b Barriers) Text(key string) (text string, ok bool) {
	switch key {
	case KeyTakeProfitBps:
		return formatBps(b.TakeProfitBps), true
	case KeyStopLossBps:
		return formatBps(b.StopLossBps), true
	case KeyTimeoutMinutes:
		return fmt.Sprintf("%d", b.TimeoutMinutes()), true
	case KeyCostBps:
		return formatBps(b.CostBps), true
	}
	return "", false
}

func formatBps(v float64) string { return fmt.Sprintf("%g", v) }

// Apply writes jsonValue - a runtime_settings.value, a JSON number - into the
// field of b named by key. It checks the single value's range only; Validate
// checks the fields together.
func Apply(b *Barriers, key, jsonValue string) error {
	if !IsKey(key) {
		return fmt.Errorf("barriersettings: %q is not a triple-barrier setting key", key)
	}
	var n float64
	if err := json.Unmarshal([]byte(jsonValue), &n); err != nil {
		return fmt.Errorf("barriersettings: decode runtime setting %s=%q as a number: %w", key, jsonValue, err)
	}
	if math.IsNaN(n) || math.IsInf(n, 0) {
		return fmt.Errorf("barriersettings: runtime setting %s=%q is not finite", key, jsonValue)
	}
	switch key {
	case KeyTakeProfitBps:
		if n <= 0 || n > MaxBps {
			return rangeError(key, jsonValue, "0 より大きく %g 以下", MaxBps)
		}
		b.TakeProfitBps = n
	case KeyStopLossBps:
		if n <= 0 || n > MaxBps {
			return rangeError(key, jsonValue, "0 より大きく %g 以下", MaxBps)
		}
		b.StopLossBps = n
	case KeyCostBps:
		if n < 0 || n > MaxBps {
			return rangeError(key, jsonValue, "0 以上 %g 以下", MaxBps)
		}
		b.CostBps = n
	case KeyTimeoutMinutes:
		if n != math.Trunc(n) || n < MinTimeoutMinutes || n > MaxTimeoutMinutes {
			return rangeError(key, jsonValue, "%d〜%d の整数", MinTimeoutMinutes, MaxTimeoutMinutes)
		}
		b.Timeout = time.Duration(n) * time.Minute
	}
	return nil
}

func rangeError(key, jsonValue, format string, args ...any) error {
	return fmt.Errorf("barriersettings: runtime setting %s=%q must be "+format, append([]any{key, jsonValue}, args...)...)
}

// Validate enforces every field's range and CostBps < StopLossBps (otherwise
// the stop-loss barrier would sit on the profit side of the entry). Violations
// are named by runtime_settings key and joined.
func (b Barriers) Validate() error {
	var errs []error
	if !(b.TakeProfitBps > 0 && b.TakeProfitBps <= MaxBps) {
		errs = append(errs, fmt.Errorf("%s は 0 より大きく %g 以下で指定してください", KeyTakeProfitBps, MaxBps))
	}
	if !(b.StopLossBps > 0 && b.StopLossBps <= MaxBps) {
		errs = append(errs, fmt.Errorf("%s は 0 より大きく %g 以下で指定してください", KeyStopLossBps, MaxBps))
	}
	if !(b.CostBps >= 0 && b.CostBps <= MaxBps) {
		errs = append(errs, fmt.Errorf("%s は 0 以上 %g 以下で指定してください", KeyCostBps, MaxBps))
	}
	if m := b.TimeoutMinutes(); b.Timeout%time.Minute != 0 || m < MinTimeoutMinutes || m > MaxTimeoutMinutes {
		errs = append(errs, fmt.Errorf("%s は %d〜%d 分の整数で指定してください", KeyTimeoutMinutes, MinTimeoutMinutes, MaxTimeoutMinutes))
	}
	if b.CostBps >= b.StopLossBps {
		errs = append(errs, fmt.Errorf("%s は %s より小さくしてください（コスト控除後の損切りが利益側になるため）", KeyCostBps, KeyStopLossBps))
	}
	return errors.Join(errs...)
}

// Store is the runtime_settings read access Load needs;
// *system.RuntimeSettingsRepository implements it.
type Store interface {
	Get(ctx context.Context, key string) (value string, ok bool, err error)
}

// Load returns the effective barriers: each stored row over its default. A
// stored set that no longer parses or validates (the Settings screen stores
// only valid ones) is logged and replaced by the defaults, so a damaged row
// cannot stop outcome labeling; only a store read failure is an error.
func Load(ctx context.Context, store Store) (Barriers, error) {
	b := Default()
	for _, key := range Keys() {
		raw, ok, err := store.Get(ctx, key)
		if err != nil {
			return Default(), fmt.Errorf("barriersettings: read %s: %w", key, err)
		}
		if !ok {
			continue
		}
		if err := Apply(&b, key, raw); err != nil {
			slog.Warn("barriersettings: ignoring invalid stored triple-barrier settings; using defaults", "error", err)
			return Default(), nil
		}
	}
	if err := b.Validate(); err != nil {
		slog.Warn("barriersettings: ignoring invalid stored triple-barrier settings; using defaults", "error", err)
		return Default(), nil
	}
	return b, nil
}
