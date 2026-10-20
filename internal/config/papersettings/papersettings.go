// Package papersettings holds the Settings-screen values of the Paper Trading
// operating capital and its two exposure limits (issue #804): the capital
// every "percentage of account equity" limit and the position sizing are
// measured against, max_position_per_symbol_pct and max_total_exposure_pct.
// They live in runtime_settings (docs/architecture/er.md §runtime_settings)
// under Keys and override config/risk.yaml's paper section key by key; a key
// with no (valid) stored row keeps the risk.yaml value, so the file stays the
// fallback default.
//
// Paper only: the Risk Engine of a Live mode never gets the overlay wired in
// (internal/bootstrap/risk.go), so these keys cannot change a Live limit.
// The Risk Engine reads the rows on every evaluation, so a saved value
// applies from the next candidate without a restart.
package papersettings

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"slices"

	"github.com/ousiassllc/pitha-trador/internal/config"
)

// runtime_settings keys; every value is a JSON number.
const (
	KeyInitialCapitalJPY       = "risk.paper.initial_capital_jpy"
	KeyMaxPositionPerSymbolPct = "risk.paper.max_position_per_symbol_pct"
	KeyMaxTotalExposurePct     = "risk.paper.max_total_exposure_pct"
)

// The Paper initial values (issue #804): 500,000 JPY of capital that may all
// go into one symbol and into total exposure. SeedDefaults stores them once
// per database and the shipped config/risk.yaml carries the same numbers.
const (
	DefaultInitialCapitalJPY       = 500_000.0
	DefaultMaxPositionPerSymbolPct = 100.0
	DefaultMaxTotalExposurePct     = 100.0
)

// MaxCapitalJPY is the largest capital Apply accepts (1 兆円), far above any
// real account and far below what could overflow the percentage arithmetic;
// it equals dailyguardsettings.MaxCapitalJPY.
const MaxCapitalJPY = 1e12

// Keys returns every runtime_settings key Apply accepts, in display order.
func Keys() []string {
	return []string{KeyInitialCapitalJPY, KeyMaxPositionPerSymbolPct, KeyMaxTotalExposurePct}
}

// IsKey reports whether key is one of Keys.
func IsKey(key string) bool { return slices.Contains(Keys(), key) }

// Overrides are the stored rows of Keys. A nil field means that key has no
// (valid) stored row.
type Overrides struct {
	InitialCapitalJPY       *float64
	MaxPositionPerSymbolPct *float64
	MaxTotalExposurePct     *float64
}

// Apply writes jsonValue - a runtime_settings.value, a JSON number - into the
// field named by key after checking its range: the capital is above 0 and at
// most MaxCapitalJPY, each percentage is above 0 and at most 100. o is left
// untouched on error.
func Apply(o *Overrides, key, jsonValue string) error {
	var n float64
	if err := json.Unmarshal([]byte(jsonValue), &n); err != nil {
		return fmt.Errorf("papersettings: decode runtime setting %s=%q as a number: %w", key, jsonValue, err)
	}
	if math.IsNaN(n) || math.IsInf(n, 0) {
		return fmt.Errorf("papersettings: runtime setting %s=%q must be a finite number", key, jsonValue)
	}
	var target **float64
	switch key {
	case KeyInitialCapitalJPY:
		if n <= 0 || n > MaxCapitalJPY {
			return fmt.Errorf("papersettings: runtime setting %s=%q: 運用資金は 0 円より大きく 1 兆円（1000000000000 円）以下で指定してください", key, jsonValue)
		}
		target = &o.InitialCapitalJPY
	case KeyMaxPositionPerSymbolPct:
		target = &o.MaxPositionPerSymbolPct
	case KeyMaxTotalExposurePct:
		target = &o.MaxTotalExposurePct
	default:
		return fmt.Errorf("papersettings: %q is not a Paper risk setting key", key)
	}
	if key != KeyInitialCapitalJPY && (n <= 0 || n > 100) {
		return fmt.Errorf("papersettings: runtime setting %s=%q must be above 0 and at most 100", key, jsonValue)
	}
	*target = &n
	return nil
}

// Overlay returns base (config/risk.yaml's Paper limits) with every stored
// value in place of its yaml counterpart. Every other limit is base's.
func (o Overrides) Overlay(base config.RiskLimits) config.RiskLimits {
	if o.InitialCapitalJPY != nil {
		base.InitialCapital = *o.InitialCapitalJPY
	}
	if o.MaxPositionPerSymbolPct != nil {
		base.MaxPositionPerSymbolPct = *o.MaxPositionPerSymbolPct
	}
	if o.MaxTotalExposurePct != nil {
		base.MaxTotalExposurePct = *o.MaxTotalExposurePct
	}
	return base
}

// Store is the runtime_settings read access Load needs;
// *system.RuntimeSettingsRepository implements it.
type Store interface {
	Get(ctx context.Context, key string) (value string, ok bool, err error)
}

// Load reads every stored row. A row that no longer parses or is out of range
// (the Settings screen stores only valid ones) is logged and treated as not
// stored, so a damaged row cannot stop trading or loosen a limit beyond the
// yaml value; only a store read failure is an error.
func Load(ctx context.Context, store Store) (Overrides, error) {
	var o Overrides
	for _, key := range Keys() {
		raw, ok, err := store.Get(ctx, key)
		if err != nil {
			return Overrides{}, fmt.Errorf("papersettings: read %s: %w", key, err)
		}
		if !ok {
			continue
		}
		if err := Apply(&o, key, raw); err != nil {
			slog.Warn("papersettings: ignoring invalid stored Paper risk setting; the config/risk.yaml value applies", "error", err)
		}
	}
	return o, nil
}

// Effective returns base with the stored Paper values overlaid (Load then
// Overlay). It is the per-evaluation read the Risk Engine, the portfolio
// provider and the Settings screen share.
func Effective(ctx context.Context, store Store, base config.RiskLimits) (config.RiskLimits, error) {
	o, err := Load(ctx, store)
	if err != nil {
		return base, err
	}
	return o.Overlay(base), nil
}

// Overlay returns the function internal/service/risk.Config.LimitsOverlay
// takes: Effective over store. Wiring it is what makes an Engine Paper-only
// configurable; an Engine built without it enforces its Limits unchanged.
func Overlay(store Store) func(ctx context.Context, base config.RiskLimits) (config.RiskLimits, error) {
	return func(ctx context.Context, base config.RiskLimits) (config.RiskLimits, error) {
		return Effective(ctx, store, base)
	}
}
