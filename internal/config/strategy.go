package config

import (
	"fmt"
	"os"
	"strconv"
)

// StrategyConfig mirrors config/strategy.yaml: Scheduler周期・Fast Screener
// しきい値・Policy Engineしきい値（docs/requirements/functional.md §4.2,
// §4.3, §4.6）。
type StrategyConfig struct {
	Scan         ScanConfig         `yaml:"scan"`
	FastScreener FastScreenerConfig `yaml:"fast_screener"`
	JevScout     JevScoutConfig     `yaml:"jev_scout"`
	Policy       PolicyConfig       `yaml:"policy"`
}

// ScanConfig holds the Scheduler scan-frequency settings
// (functional.md §4.3 スキャン頻度・イベント駆動).
type ScanConfig struct {
	FullScanIntervalSeconds            int                `yaml:"full_scan_interval_seconds"`
	CandidateRefreshIntervalSecondsMin int                `yaml:"candidate_refresh_interval_seconds_min"`
	CandidateRefreshIntervalSecondsMax int                `yaml:"candidate_refresh_interval_seconds_max"`
	HeldPositionIntervalSecondsMin     int                `yaml:"held_position_interval_seconds_min"`
	HeldPositionIntervalSecondsMax     int                `yaml:"held_position_interval_seconds_max"`
	EventTrigger                       EventTriggerConfig `yaml:"event_trigger"`
}

// EventTriggerConfig holds FR-SCAN-1/FR-SCAN-2's event-driven
// re-evaluation thresholds (functional.md §4.3): a symbol whose
// abs(1分リターン), abs(出来高比), abs(スプレッドΔbps) and
// abs(板インバランスΔ) and abs(約定フロー不均衡Δ) all stay under their
// respective threshold, and has no VWAP cross/high-low break/news flag either,
// is quiet enough that Jev Scout evaluation should be skipped that cycle
// (FR-SCAN-2); otherwise the symbol is re-evaluated immediately,
// bypassing the normal candidate-refresh cadence (FR-SCAN-1). The
// caller translates these into
// internal/service/featureengine/eventtrigger.Thresholds (that package cannot
// import internal/config directly, doc.go's layer rule).
// functional.md §4.3 fixes no numeric default for these - unlike Fast
// Screener/Risk's thresholds - so config/strategy.yaml's values are this
// project's own initial tuning pass, adjustable post-backtest like every
// other threshold in this file. Detect compares Return1mChangeThreshold
// with abs(return_1m) and VolumeRatioChangeThreshold with
// abs(volume_ratio_5m) - current levels, not bar-to-bar deltas - and the
// spread/orderbook/trade-flow thresholds with abs(curr - prev). Either way it
// fires on >= threshold, so an unset (0) threshold would fire on every bar; the loaders replace
// unset/non-positive values with the shipped defaults
// (withEventTriggerDefaults).
type EventTriggerConfig struct {
	Return1mChangeThreshold           float64 `yaml:"return_1m_change_threshold"`
	VolumeRatioChangeThreshold        float64 `yaml:"volume_ratio_change_threshold"`
	SpreadChangeBpsThreshold          float64 `yaml:"spread_change_bps_threshold"`
	OrderbookImbalanceChangeThreshold float64 `yaml:"orderbook_imbalance_change_threshold"`
	TradeFlowImbalanceChangeThreshold float64 `yaml:"trade_flow_imbalance_change_threshold"`
}

// FastScreenerConfig holds the Fast Screener numeric filters and
// screen_score weights (functional.md §4.2, FR-FS-1/FR-FS-2).
type FastScreenerConfig struct {
	MinPrice         float64 `yaml:"min_price"`
	MaxPrice         float64 `yaml:"max_price"`
	MinTurnover5mJPY float64 `yaml:"min_turnover_5m_jpy"`
	MaxSpreadBps     float64 `yaml:"max_spread_bps"`
	MinVolumeRatio   float64 `yaml:"min_volume_ratio"`
	// MinAbsReturn5mPct is in percent (0.3 == 0.3%), unlike Feature.Return5m
	// which is a decimal ratio; screener converts via domain.RatioToPercent.
	MinAbsReturn5mPct     float64             `yaml:"min_abs_return_5m_pct"`
	MinRealizedVolatility float64             `yaml:"min_realized_volatility"`
	TopN                  int                 `yaml:"top_n"`
	Weights               FastScreenerWeights `yaml:"weights"`
}

// FastScreenerWeights holds the screen_score component weights
// (functional.md §4.2, FR-FS-2).
type FastScreenerWeights struct {
	VolumeRatio         float64 `yaml:"volume_ratio"`
	AbsReturn5m         float64 `yaml:"abs_return_5m"`
	BreakoutStrength    float64 `yaml:"breakout_strength"`
	OrderbookImbalance  float64 `yaml:"orderbook_imbalance"`
	VolatilityExpansion float64 `yaml:"volatility_expansion"`
}

// JevScoutConfig holds the Jev Scout FR-SCOUT-2 pass-condition thresholds:
// the minimum yes-probability each of the three gating questions must
// clear (functional.md §4.4). All three are required (AND); values are
// configurable rather than hardcoded so they can be tuned after
// backtesting without a code change.
type JevScoutConfig struct {
	MinInterestingNow   float64 `yaml:"min_interesting_now"`
	MinLiquidityOk      float64 `yaml:"min_liquidity_ok"`
	MinAbnormalActivity float64 `yaml:"min_abnormal_activity"`
}

// PolicyConfig holds the Policy Engine LONG/SHORT thresholds
// (functional.md §4.6, FR-POLICY-1/FR-POLICY-2).
type PolicyConfig struct {
	Long  PolicyDirectionThresholds `yaml:"long"`
	Short PolicyDirectionThresholds `yaml:"short"`

	// MinCalibrationSamples is the fewest labeled Calibration samples the
	// confidence bucket of a Jev decision must hold for that decision to
	// count as calibrated; below it FR-POLICY-3's "キャリブレーション
	// 対象外" applies and the signal is NONE. 0 disables the check.
	MinCalibrationSamples int `yaml:"min_calibration_samples"`
}

// PolicyDirectionThresholds holds the thresholds a Jev Trader decision must
// clear, for one direction (LONG or SHORT), to become a trade signal.
type PolicyDirectionThresholds struct {
	MinProbability             float64 `yaml:"min_probability"`
	MinEntryQuality            string  `yaml:"min_entry_quality"`
	MinContinuationProbability float64 `yaml:"min_continuation_probability"`
	MaxToxicFlow               float64 `yaml:"max_toxic_flow"`
	MaxLiquidityStressed       float64 `yaml:"max_liquidity_stressed"`
}

// LoadStrategy reads and parses the strategy configuration YAML file at
// path (conventionally DefaultStrategyPath) into a StrategyConfig, then
// applies any PITHA_POLICY_LONG_*/PITHA_POLICY_SHORT_* (FR-POLICY-4) and
// PITHA_FAST_SCREENER_* (FR-FS-1/FR-FS-3) environment variable overrides
// on top of it. runtime_settings-backed overrides are layered on top of
// this result at read time: policy.* by internal/service/selfimprove.
// RuntimePolicy, screener.* by ApplyFastScreenerSetting
// (docs/architecture/er.md §runtime_settings).
func LoadStrategy(path string) (*StrategyConfig, error) {
	cfg, err := loadYAMLFile[StrategyConfig](path)
	if err != nil {
		return nil, err
	}
	if err := applyEnvOverrides(cfg); err != nil {
		return nil, err
	}
	withScanIntervalDefaults(&cfg.Scan)
	withEventTriggerDefaults(&cfg.Scan.EventTrigger)
	return cfg, nil
}

// LoadStrategyBytes parses data (conventionally an embedded copy of
// config/strategy.yaml, github.com/ousiassllc/pitha-trador/config's
// configdefaults.DefaultStrategyYAML) as a StrategyConfig, applying the
// same environment overrides as LoadStrategy. internal/bootstrap falls back to this when no
// strategy.yaml is found on disk (explicit path, PITHA_STRATEGY_PATH, nor
// next to the running executable) so a distributed .exe with no
// accompanying config/ directory still starts.
func LoadStrategyBytes(data []byte) (*StrategyConfig, error) {
	cfg, err := loadYAMLBytes[StrategyConfig](data, "(embedded default)")
	if err != nil {
		return nil, err
	}
	if err := applyEnvOverrides(cfg); err != nil {
		return nil, err
	}
	withScanIntervalDefaults(&cfg.Scan)
	withEventTriggerDefaults(&cfg.Scan.EventTrigger)
	return cfg, nil
}

func applyEnvOverrides(cfg *StrategyConfig) error {
	if err := applyPolicyEnvOverrides(&cfg.Policy); err != nil {
		return err
	}
	return applyFastScreenerEnvOverrides(&cfg.FastScreener)
}

func applyPolicyEnvOverrides(cfg *PolicyConfig) error {
	if err := applyPolicyDirectionEnvOverrides("PITHA_POLICY_LONG_", &cfg.Long); err != nil {
		return err
	}
	return applyPolicyDirectionEnvOverrides("PITHA_POLICY_SHORT_", &cfg.Short)
}

func applyPolicyDirectionEnvOverrides(prefix string, t *PolicyDirectionThresholds) error {
	if err := envFloatOverride(prefix+"MIN_PROBABILITY", &t.MinProbability); err != nil {
		return err
	}
	if v, ok := os.LookupEnv(prefix + "MIN_ENTRY_QUALITY"); ok {
		t.MinEntryQuality = v
	}
	if err := envFloatOverride(prefix+"MIN_CONTINUATION_PROBABILITY", &t.MinContinuationProbability); err != nil {
		return err
	}
	if err := envFloatOverride(prefix+"MAX_TOXIC_FLOW", &t.MaxToxicFlow); err != nil {
		return err
	}
	return envFloatOverride(prefix+"MAX_LIQUIDITY_STRESSED", &t.MaxLiquidityStressed)
}

// envFloatOverride sets *dest to the value of the environment variable
// key, parsed as a float64, when key is set; it is a no-op when key is
// unset.
func envFloatOverride(key string, dest *float64) error {
	v, ok := os.LookupEnv(key)
	if !ok {
		return nil
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return fmt.Errorf("config: parse %s=%q as float: %w", key, v, err)
	}
	*dest = f
	return nil
}
