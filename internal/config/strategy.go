package config

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
	FullScanIntervalSeconds            int `yaml:"full_scan_interval_seconds"`
	CandidateRefreshIntervalSecondsMin int `yaml:"candidate_refresh_interval_seconds_min"`
	CandidateRefreshIntervalSecondsMax int `yaml:"candidate_refresh_interval_seconds_max"`
	HeldPositionIntervalSecondsMin     int `yaml:"held_position_interval_seconds_min"`
	HeldPositionIntervalSecondsMax     int `yaml:"held_position_interval_seconds_max"`
}

// FastScreenerConfig holds the Fast Screener numeric filters and
// screen_score weights (functional.md §4.2, FR-FS-1/FR-FS-2).
type FastScreenerConfig struct {
	MinPrice              float64             `yaml:"min_price"`
	MaxPrice              float64             `yaml:"max_price"`
	MinTurnover5mJPY      float64             `yaml:"min_turnover_5m_jpy"`
	MaxSpreadBps          float64             `yaml:"max_spread_bps"`
	MinVolumeRatio        float64             `yaml:"min_volume_ratio"`
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
// path (conventionally DefaultStrategyPath) into a StrategyConfig.
func LoadStrategy(path string) (*StrategyConfig, error) {
	return loadYAMLFile[StrategyConfig](path)
}
