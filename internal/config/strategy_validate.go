package config

import (
	"errors"
	"fmt"
)

// entryQualityValues are the JevEntryQuality* strings a
// policy.{long,short}.min_entry_quality may take (FR-POLICY-1/2). This
// package must not import internal/domain (doc.go's layer rule), so the
// list is mirrored here; TestEntryQualityValues_MatchDomain keeps it in sync
// with domain.JevEntryQuality*.
var entryQualityValues = []string{"poor", "fair", "good", "strong", "exceptional"}

// Validate rejects strategy.yaml values that would make Fast Screener, Jev
// Scout or Policy Engine fail open or panic. A key omitted from strategy.yaml
// (or a typo'd one) decodes as 0 / "", and for these sections that is never
// a sensible value: top_n <= 0 empties (or, when negative, panics) the
// candidate list, jev_scout.min_* = 0 passes every symbol on to Jev Trader,
// and an unknown policy.*.min_entry_quality ranks as "poor", disabling
// FR-POLICY-1/2's entry_quality gate (functional/components-pipeline.md
// §4.2/§4.4/§4.6). Unlike scan.* (withScanIntervalDefaults), these are not
// back-filled with defaults: they gate real trades, so a missing/invalid
// value fails startup rather than being silently replaced. Every violation
// is reported at once, keyed by its YAML path.
//
// LoadStrategy/LoadStrategyBytes call it after the PITHA_POLICY_* and
// PITHA_FAST_SCREENER_* environment overrides have been applied, so an
// invalid override is rejected too. scan.* is not checked here: it is
// default-filled by the loaders beforehand.
func (c *StrategyConfig) Validate() error {
	errs := c.FastScreener.validate("fast_screener")
	errs = append(errs, c.JevScout.validate("jev_scout")...)
	errs = append(errs, c.Policy.validate("policy")...)
	if len(errs) == 0 {
		return nil
	}
	return fmt.Errorf("config: invalid strategy.yaml: %w", errors.Join(errs...))
}

func (c FastScreenerConfig) validate(key string) []error {
	var errs []error
	positive := func(name string, v float64) {
		if !(v > 0) { // also rejects NaN
			errs = append(errs, fmt.Errorf("%s.%s must be > 0 (got %v)", key, name, v))
		}
	}
	positive("min_price", c.MinPrice)
	positive("max_price", c.MaxPrice)
	if c.MinPrice > 0 && c.MaxPrice > 0 && c.MaxPrice < c.MinPrice {
		errs = append(errs, fmt.Errorf("%s.max_price must be >= min_price (got max_price=%v, min_price=%v)", key, c.MaxPrice, c.MinPrice))
	}
	positive("min_turnover_5m_jpy", c.MinTurnover5mJPY)
	positive("max_spread_bps", c.MaxSpreadBps)
	positive("min_volume_ratio", c.MinVolumeRatio)
	positive("min_abs_return_5m_pct", c.MinAbsReturn5mPct)
	positive("min_realized_volatility", c.MinRealizedVolatility)
	if c.TopN < 1 {
		errs = append(errs, fmt.Errorf("%s.top_n must be >= 1 (got %d)", key, c.TopN))
	}
	return append(errs, c.Weights.validate(key+".weights")...)
}

// validate requires every weight to be >= 0 and at least one > 0: an
// all-zero (e.g. omitted) weights block gives every symbol screen_score 0,
// so top_n would keep an arbitrary subset (FR-FS-2).
func (w FastScreenerWeights) validate(key string) []error {
	var errs []error
	sum := 0.0
	for _, f := range []struct {
		name string
		v    float64
	}{
		{"volume_ratio", w.VolumeRatio},
		{"abs_return_5m", w.AbsReturn5m},
		{"breakout_strength", w.BreakoutStrength},
		{"orderbook_imbalance", w.OrderbookImbalance},
		{"volatility_expansion", w.VolatilityExpansion},
	} {
		if !(f.v >= 0) { // also rejects NaN
			errs = append(errs, fmt.Errorf("%s.%s must be >= 0 (got %v)", key, f.name, f.v))
			continue
		}
		sum += f.v
	}
	if len(errs) == 0 && !(sum > 0) {
		errs = append(errs, fmt.Errorf("%s must have at least one weight > 0", key))
	}
	return errs
}

func (c JevScoutConfig) validate(key string) []error {
	var errs []error
	errs = appendProbabilityErr(errs, key+".min_interesting_now", c.MinInterestingNow)
	errs = appendProbabilityErr(errs, key+".min_liquidity_ok", c.MinLiquidityOk)
	return appendProbabilityErr(errs, key+".min_abnormal_activity", c.MinAbnormalActivity)
}

func (c PolicyConfig) validate(key string) []error {
	errs := c.Long.validate(key + ".long")
	errs = append(errs, c.Short.validate(key+".short")...)
	if c.MinCalibrationSamples < 0 { // 0 disables the check
		errs = append(errs, fmt.Errorf("%s.min_calibration_samples must be >= 0 (got %d)", key, c.MinCalibrationSamples))
	}
	return errs
}

func (t PolicyDirectionThresholds) validate(key string) []error {
	var errs []error
	errs = appendProbabilityErr(errs, key+".min_probability", t.MinProbability)
	errs = appendProbabilityErr(errs, key+".min_continuation_probability", t.MinContinuationProbability)
	errs = appendProbabilityErr(errs, key+".max_toxic_flow", t.MaxToxicFlow)
	errs = appendProbabilityErr(errs, key+".max_liquidity_stressed", t.MaxLiquidityStressed)
	if !isEntryQuality(t.MinEntryQuality) {
		errs = append(errs, fmt.Errorf("%s.min_entry_quality must be one of %v (got %q)", key, entryQualityValues, t.MinEntryQuality))
	}
	return errs
}

// IsProbabilityThreshold reports whether v is a valid policy/screener
// probability threshold: inside (0, 1] (NaN is rejected). domain's
// self-improvement validation reuses it so the startup and runtime ranges
// cannot drift apart.
func IsProbabilityThreshold(v float64) bool {
	return v > 0 && v <= 1
}

// appendProbabilityErr appends an error naming key when v is outside (0, 1].
func appendProbabilityErr(errs []error, key string, v float64) []error {
	if !IsProbabilityThreshold(v) {
		return append(errs, fmt.Errorf("%s must be in (0, 1] (got %v)", key, v))
	}
	return errs
}

func isEntryQuality(s string) bool {
	for _, v := range entryQualityValues {
		if s == v {
			return true
		}
	}
	return false
}
