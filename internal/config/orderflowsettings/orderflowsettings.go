// Package orderflowsettings holds the Settings-screen configuration of the
// Order Flow Imbalance windows (issue #750): the three trailing window widths
// (default 10s / 30s / 60s) over which internal/service/orderflow sums OFI.
// They live in runtime_settings (docs/architecture/er.md §runtime_settings)
// under Keys; there is no yaml or environment source.
package orderflowsettings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

// runtime_settings keys; each value is a JSON integer number of seconds.
const (
	KeyShort = "orderflow.ofi_window_short_seconds"
	KeyMid   = "orderflow.ofi_window_mid_seconds"
	KeyLong  = "orderflow.ofi_window_long_seconds"
)

// Default window widths.
const (
	DefaultShort = 10 * time.Second
	DefaultMid   = 30 * time.Second
	DefaultLong  = 60 * time.Second
)

// MaxWindow is the widest window allowed: internal/service/orderflow keeps
// this much history per symbol.
const MaxWindow = 10 * time.Minute

// Windows are the three OFI window widths, Short < Mid < Long.
type Windows struct {
	Short, Mid, Long time.Duration
}

// Default returns the shipped 10s / 30s / 60s windows.
func Default() Windows { return Windows{Short: DefaultShort, Mid: DefaultMid, Long: DefaultLong} }

// Keys returns every runtime_settings key Apply accepts, short to long.
func Keys() []string { return []string{KeyShort, KeyMid, KeyLong} }

// IsKey reports whether key is one of Keys.
func IsKey(key string) bool { return key == KeyShort || key == KeyMid || key == KeyLong }

func (w *Windows) field(key string) *time.Duration {
	switch key {
	case KeyShort:
		return &w.Short
	case KeyMid:
		return &w.Mid
	case KeyLong:
		return &w.Long
	}
	return nil
}

// Seconds returns w's width for key in whole seconds, or ok=false when key is
// not one of Keys.
func (w Windows) Seconds(key string) (seconds int, ok bool) {
	d := w.field(key)
	if d == nil {
		return 0, false
	}
	return int(*d / time.Second), true
}

// Apply writes jsonValue - a runtime_settings.value, a JSON integer number of
// seconds - into w's width named by key. It checks the single value's range
// only; Validate checks the three together.
func Apply(w *Windows, key, jsonValue string) error {
	dest := w.field(key)
	if dest == nil {
		return fmt.Errorf("orderflowsettings: %q is not an OFI window setting key", key)
	}
	var n int64
	if err := json.Unmarshal([]byte(jsonValue), &n); err != nil {
		return fmt.Errorf("orderflowsettings: decode runtime setting %s=%q as integer seconds: %w", key, jsonValue, err)
	}
	if n < 1 || time.Duration(n)*time.Second > MaxWindow {
		return fmt.Errorf("orderflowsettings: runtime setting %s=%q must be 1〜%d seconds", key, jsonValue, int(MaxWindow/time.Second))
	}
	*dest = time.Duration(n) * time.Second
	return nil
}

// Validate enforces 1s <= Short < Mid < Long <= MaxWindow. Violations are
// named by runtime_settings key and joined.
func (w Windows) Validate() error {
	var errs []error
	for _, key := range Keys() {
		d := *w.field(key)
		if d < time.Second || d > MaxWindow {
			errs = append(errs, fmt.Errorf("%s は 1〜%d 秒で指定してください", key, int(MaxWindow/time.Second)))
		}
	}
	if w.Short >= w.Mid {
		errs = append(errs, fmt.Errorf("%s は %s より小さくしてください", KeyShort, KeyMid))
	}
	if w.Mid >= w.Long {
		errs = append(errs, fmt.Errorf("%s は %s より小さくしてください", KeyMid, KeyLong))
	}
	return errors.Join(errs...)
}

// Store is the runtime_settings read access Load needs;
// *system.RuntimeSettingsRepository implements it.
type Store interface {
	Get(ctx context.Context, key string) (value string, ok bool, err error)
}

// Load returns the effective windows: each stored row over its default. A
// stored set that no longer parses or validates (the Settings screen stores
// only valid ones) is logged and replaced by the defaults, so a damaged row
// cannot stop market data; only a store read failure is an error.
func Load(ctx context.Context, store Store) (Windows, error) {
	w := Default()
	for _, key := range Keys() {
		raw, ok, err := store.Get(ctx, key)
		if err != nil {
			return Default(), fmt.Errorf("orderflowsettings: read %s: %w", key, err)
		}
		if !ok {
			continue
		}
		if err := Apply(&w, key, raw); err != nil {
			slog.Warn("orderflowsettings: ignoring invalid stored OFI windows; using defaults", "error", err)
			return Default(), nil
		}
	}
	if err := w.Validate(); err != nil {
		slog.Warn("orderflowsettings: ignoring invalid stored OFI windows; using defaults", "error", err)
		return Default(), nil
	}
	return w, nil
}
