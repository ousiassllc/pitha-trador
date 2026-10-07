package config

import "time"

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
	// FullScanEnabled turns the 60秒 full REST scan (the market-data job for
	// every active instrument, FR-SCHED-2) on or off. nil (key omitted) means
	// off: only an explicit true enables it; read it via FullScanOn. Off, the
	// ranking-based watchlist (internal/bootstrap/rankingwatch, FR-SCHED-9)
	// decides which symbols are ingested (issues #651/#652).
	FullScanEnabled                    *bool `yaml:"full_scan_enabled"`
	FullScanIntervalSeconds            int   `yaml:"full_scan_interval_seconds"`
	CandidateRefreshIntervalSecondsMin int   `yaml:"candidate_refresh_interval_seconds_min"`
	CandidateRefreshIntervalSecondsMax int   `yaml:"candidate_refresh_interval_seconds_max"`
	HeldPositionIntervalSecondsMin     int   `yaml:"held_position_interval_seconds_min"`
	HeldPositionIntervalSecondsMax     int   `yaml:"held_position_interval_seconds_max"`
	JevScoutMinIntervalSeconds         int   `yaml:"jev_scout_min_interval_seconds"`
	// FullScanMaxSnapshotAgeSeconds is how old a market_snapshots bar may be
	// during a session before it is stale (stale_snapshot, issue #686) when
	// full_scan_enabled is true: a REST symbol is refreshed only once per
	// full cycle (about 8 minutes at 8 calls/s over ~4,000 symbols), so the
	// ranking-watch 3 minutes (domain.MaxSnapshotAge) would drop most of
	// them. Unset/non-positive is derived from the rate cap
	// (withScanIntervalDefaults); ranking-watch mode ignores it.
	FullScanMaxSnapshotAgeSeconds int `yaml:"full_scan_max_snapshot_age_seconds"`
	// KabuInfoAPIMaxPerSecond is the process-wide cap on kabuステーション
	// 情報API / 銘柄登録API calls (GetBoard, GetSymbol, RegisterSymbols).
	// Official cap is 10/s; default 8. Values above 10 are clamped.
	KabuInfoAPIMaxPerSecond int                  `yaml:"kabu_info_api_max_per_second"`
	EventTrigger            EventTriggerConfig   `yaml:"event_trigger"`
	RankingMeasure          RankingMeasureConfig `yaml:"ranking_measure"`
}

// FullScanOn reports whether the 60秒 full REST scan is enabled: false unless
// scan.full_scan_enabled is explicitly true.
func (c ScanConfig) FullScanOn() bool { return c.FullScanEnabled != nil && *c.FullScanEnabled }

// SnapshotMaxAge returns how old a bar may be during a session before it is
// stale: rankingWatchAge (domain.MaxSnapshotAge) in ranking-watch mode, and
// the longer full-scan age when full scan is on. An unset full-scan age
// (a ScanConfig that bypassed the loader's defaults) falls back to
// rankingWatchAge.
func (c ScanConfig) SnapshotMaxAge(rankingWatchAge time.Duration) time.Duration {
	if c.FullScanOn() && c.FullScanMaxSnapshotAgeSeconds > 0 {
		return time.Duration(c.FullScanMaxSnapshotAgeSeconds) * time.Second
	}
	return rankingWatchAge
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
// path (conventionally DefaultStrategyPath) into a StrategyConfig, fills
// unset scan.* values with their defaults, and finally fails with an error
// naming every offending key when fast_screener.*, jev_scout.* or policy.*
// is missing/out of range (see StrategyConfig.Validate). There is no
// environment-variable override layer: runtime_settings-backed overrides
// (editable from the Settings screen, issue #708) are layered on top of
// this result at read time - policy.* by internal/service/selfimprove.
// RuntimePolicy, screener.* by ApplyFastScreenerSetting
// (docs/architecture/er.md §runtime_settings), so the priority is
// config/strategy.yaml < runtime_settings.
func LoadStrategy(path string) (*StrategyConfig, error) {
	cfg, err := loadYAMLFile[StrategyConfig](path)
	return finishStrategy(cfg, err)
}

// LoadStrategyBytes parses data (conventionally an embedded copy of
// config/strategy.yaml, github.com/ousiassllc/pitha-trador/config's
// configdefaults.DefaultStrategyYAML) as a StrategyConfig, applying the
// same defaults and validation as LoadStrategy.
// internal/bootstrap falls back to this when no
// strategy.yaml is found on disk (explicit path, PITHA_STRATEGY_PATH, nor
// next to the running executable) so a distributed .exe with no
// accompanying config/ directory still starts.
func LoadStrategyBytes(data []byte) (*StrategyConfig, error) {
	cfg, err := loadYAMLBytes[StrategyConfig](data, "(embedded default)")
	return finishStrategy(cfg, err)
}

// finishStrategy applies LoadStrategy/LoadStrategyBytes' shared post-parse
// steps: scan.* defaults, then validation.
func finishStrategy(cfg *StrategyConfig, err error) (*StrategyConfig, error) {
	if err != nil {
		return nil, err
	}
	withScanIntervalDefaults(&cfg.Scan)
	withEventTriggerDefaults(&cfg.Scan.EventTrigger)
	withRankingMeasureDefaults(&cfg.Scan.RankingMeasure)
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}
