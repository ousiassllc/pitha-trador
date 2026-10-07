package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
)

// fastScreenerField describes one FR-FS-1/FR-FS-3 tunable: the
// runtime_settings key (docs/architecture/er.md §runtime_settings, e.g.
// "screener.min_price"), how to read it from a FastScreenerConfig and how
// to write a parsed number into one.
type fastScreenerField struct {
	key string
	get func(FastScreenerConfig) float64
	set func(cfg *FastScreenerConfig, v float64) error
}

// floatField is the field whose value is the float64 dest points at.
func floatField(key string, dest func(*FastScreenerConfig) *float64) fastScreenerField {
	return fastScreenerField{
		key: key,
		get: func(c FastScreenerConfig) float64 { return *dest(&c) },
		set: func(c *FastScreenerConfig, v float64) error {
			*dest(c) = v
			return nil
		},
	}
}

var topNField = fastScreenerField{
	key: "screener.top_n",
	get: func(c FastScreenerConfig) float64 { return float64(c.TopN) },
	set: func(c *FastScreenerConfig, v float64) error {
		if v != math.Trunc(v) || v < 0 || v > math.MaxInt32 {
			return fmt.Errorf("must be a non-negative integer, got %v", v)
		}
		c.TopN = int(v)
		return nil
	},
}

var fastScreenerFields = []fastScreenerField{
	floatField("screener.min_price", func(c *FastScreenerConfig) *float64 { return &c.MinPrice }),
	floatField("screener.max_price", func(c *FastScreenerConfig) *float64 { return &c.MaxPrice }),
	floatField("screener.min_turnover_5m_jpy", func(c *FastScreenerConfig) *float64 { return &c.MinTurnover5mJPY }),
	floatField("screener.max_spread_bps", func(c *FastScreenerConfig) *float64 { return &c.MaxSpreadBps }),
	floatField("screener.min_volume_ratio", func(c *FastScreenerConfig) *float64 { return &c.MinVolumeRatio }),
	floatField("screener.min_abs_return_5m_pct", func(c *FastScreenerConfig) *float64 { return &c.MinAbsReturn5mPct }),
	floatField("screener.min_realized_volatility", func(c *FastScreenerConfig) *float64 { return &c.MinRealizedVolatility }),
	topNField,
	floatField("screener.weights.volume_ratio", func(c *FastScreenerConfig) *float64 { return &c.Weights.VolumeRatio }),
	floatField("screener.weights.abs_return_5m", func(c *FastScreenerConfig) *float64 { return &c.Weights.AbsReturn5m }),
	floatField("screener.weights.breakout_strength", func(c *FastScreenerConfig) *float64 { return &c.Weights.BreakoutStrength }),
	floatField("screener.weights.orderbook_imbalance", func(c *FastScreenerConfig) *float64 { return &c.Weights.OrderbookImbalance }),
	floatField("screener.weights.volatility_expansion", func(c *FastScreenerConfig) *float64 { return &c.Weights.VolatilityExpansion }),
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

// FastScreenerSettingValue returns cfg's current value for key (one of
// FastScreenerSettingKeys; top_n as its integer value), or ok=false when key
// is unknown.
func FastScreenerSettingValue(cfg FastScreenerConfig, key string) (value float64, ok bool) {
	for _, f := range fastScreenerFields {
		if f.key == key {
			return f.get(cfg), true
		}
	}
	return 0, false
}

// ApplyFastScreenerSetting writes jsonValue - a runtime_settings.value
// (er.md: JSON scalar) - into cfg's field named by key, one of
// FastScreenerSettingKeys (FR-FS-3: runtime_settings overrides YAML).
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
// overrides were applied to it. LoadStrategy validates the YAML result the
// same way; this gives the higher-priority runtime_settings layer (the
// Settings screen, issue #708) the same check (issue #617; top_n >= 1 is
// checked here, not by setTopN). Violations are named by their
// runtime_settings key prefix ("screener.min_price must be > 0 ...") and
// joined.
func ValidateFastScreenerOverrides(cfg FastScreenerConfig) error {
	return errors.Join(cfg.validate("screener")...)
}
