package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"strconv"
)

// fastScreenerField describes one FR-FS-1/FR-FS-3 tunable: the
// runtime_settings key (docs/architecture/er.md §runtime_settings, e.g.
// "screener.min_price"), the PITHA_FAST_SCREENER_* environment variable,
// and how to write a parsed number into a FastScreenerConfig.
type fastScreenerField struct {
	key string
	env string
	set func(cfg *FastScreenerConfig, v float64) error
}

func floatField(dest func(*FastScreenerConfig) *float64) func(*FastScreenerConfig, float64) error {
	return func(cfg *FastScreenerConfig, v float64) error {
		*dest(cfg) = v
		return nil
	}
}

func setTopN(cfg *FastScreenerConfig, v float64) error {
	if v != math.Trunc(v) || v < 0 || v > math.MaxInt32 {
		return fmt.Errorf("must be a non-negative integer, got %v", v)
	}
	cfg.TopN = int(v)
	return nil
}

var fastScreenerFields = []fastScreenerField{
	{"screener.min_price", "PITHA_FAST_SCREENER_MIN_PRICE", floatField(func(c *FastScreenerConfig) *float64 { return &c.MinPrice })},
	{"screener.max_price", "PITHA_FAST_SCREENER_MAX_PRICE", floatField(func(c *FastScreenerConfig) *float64 { return &c.MaxPrice })},
	{"screener.min_turnover_5m_jpy", "PITHA_FAST_SCREENER_MIN_TURNOVER_5M_JPY", floatField(func(c *FastScreenerConfig) *float64 { return &c.MinTurnover5mJPY })},
	{"screener.max_spread_bps", "PITHA_FAST_SCREENER_MAX_SPREAD_BPS", floatField(func(c *FastScreenerConfig) *float64 { return &c.MaxSpreadBps })},
	{"screener.min_volume_ratio", "PITHA_FAST_SCREENER_MIN_VOLUME_RATIO", floatField(func(c *FastScreenerConfig) *float64 { return &c.MinVolumeRatio })},
	{"screener.min_abs_return_5m_pct", "PITHA_FAST_SCREENER_MIN_ABS_RETURN_5M_PCT", floatField(func(c *FastScreenerConfig) *float64 { return &c.MinAbsReturn5mPct })},
	{"screener.min_realized_volatility", "PITHA_FAST_SCREENER_MIN_REALIZED_VOLATILITY", floatField(func(c *FastScreenerConfig) *float64 { return &c.MinRealizedVolatility })},
	{"screener.top_n", "PITHA_FAST_SCREENER_TOP_N", setTopN},
	{"screener.weights.volume_ratio", "PITHA_FAST_SCREENER_WEIGHT_VOLUME_RATIO", floatField(func(c *FastScreenerConfig) *float64 { return &c.Weights.VolumeRatio })},
	{"screener.weights.abs_return_5m", "PITHA_FAST_SCREENER_WEIGHT_ABS_RETURN_5M", floatField(func(c *FastScreenerConfig) *float64 { return &c.Weights.AbsReturn5m })},
	{"screener.weights.breakout_strength", "PITHA_FAST_SCREENER_WEIGHT_BREAKOUT_STRENGTH", floatField(func(c *FastScreenerConfig) *float64 { return &c.Weights.BreakoutStrength })},
	{"screener.weights.orderbook_imbalance", "PITHA_FAST_SCREENER_WEIGHT_ORDERBOOK_IMBALANCE", floatField(func(c *FastScreenerConfig) *float64 { return &c.Weights.OrderbookImbalance })},
	{"screener.weights.volatility_expansion", "PITHA_FAST_SCREENER_WEIGHT_VOLATILITY_EXPANSION", floatField(func(c *FastScreenerConfig) *float64 { return &c.Weights.VolatilityExpansion })},
}

// FastScreenerSettingKeys returns every runtime_settings key
// ApplyFastScreenerSetting accepts (FR-FS-3), in a stable order.
func FastScreenerSettingKeys() []string {
	keys := make([]string, len(fastScreenerFields))
	for i, f := range fastScreenerFields {
		keys[i] = f.key
	}
	return keys
}

// applyFastScreenerEnvOverrides overrides cfg's fields from the
// PITHA_FAST_SCREENER_* environment variables that are set (FR-FS-1/
// FR-FS-3). Unset variables leave the YAML value untouched.
func applyFastScreenerEnvOverrides(cfg *FastScreenerConfig) error {
	for _, f := range fastScreenerFields {
		raw, ok := os.LookupEnv(f.env)
		if !ok {
			continue
		}
		v, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return fmt.Errorf("config: parse %s=%q as float: %w", f.env, raw, err)
		}
		if err := f.set(cfg, v); err != nil {
			return fmt.Errorf("config: %s: %w", f.env, err)
		}
	}
	return nil
}

// ApplyFastScreenerSetting writes jsonValue - a runtime_settings.value
// (er.md: JSON scalar) - into cfg's field named by key, one of
// FastScreenerSettingKeys (FR-FS-3: DB overrides env and YAML).
func ApplyFastScreenerSetting(cfg *FastScreenerConfig, key, jsonValue string) error {
	for _, f := range fastScreenerFields {
		if f.key != key {
			continue
		}
		var v float64
		if err := json.Unmarshal([]byte(jsonValue), &v); err != nil {
			return fmt.Errorf("config: decode runtime setting %s=%q as number: %w", key, jsonValue, err)
		}
		if err := f.set(cfg, v); err != nil {
			return fmt.Errorf("config: runtime setting %s=%q: %w", key, jsonValue, err)
		}
		return nil
	}
	return fmt.Errorf("config: %q is not a fast screener runtime setting key", key)
}

// ValidateFastScreenerOverrides enforces FR-FS-4's invariants (every
// threshold > 0, max_price >= min_price, top_n >= 1, weights all >= 0 with a
// positive sum) on a FastScreenerConfig after runtime_settings screener.*
// overrides were applied to it. LoadStrategy validates the YAML+env result
// the same way; this gives the highest-priority DB layer the same check
// (issue #617; top_n >= 1 is checked here, not by setTopN, so an env value
// of 0 is reported by LoadStrategy's validation). Violations are named by their runtime_settings key prefix
// ("screener.min_price must be > 0 ...") and joined.
func ValidateFastScreenerOverrides(cfg FastScreenerConfig) error {
	return errors.Join(cfg.validate("screener")...)
}
