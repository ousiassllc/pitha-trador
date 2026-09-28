package config

// RiskConfig mirrors config/risk.yaml: Risk Engine制限値（Paper/Live別、
// docs/requirements/functional.md §4.7 Risk Engine の表）。
//
// risk.yaml is not writable by the self-improvement loop
// (docs/architecture/overview.md §1「AI自己改善ループの境界」); only a
// human operator changes these values.
type RiskConfig struct {
	Paper RiskLimits `yaml:"paper"`
	Live  RiskLimits `yaml:"live"`
}

// RiskLimits holds one operating mode's (Paper or Live) Risk Engine limits.
// HeartbeatTimeoutMinutes is Live-only (dead-man's switch, FR-RISK-6); it is
// 0 for Paper.
type RiskLimits struct {
	MaxPositionPerSymbolPct           float64 `yaml:"max_position_per_symbol_pct"`
	MaxTotalExposurePct               float64 `yaml:"max_total_exposure_pct"`
	MaxDailyLossPct                   float64 `yaml:"max_daily_loss_pct"`
	MaxTradeLossPct                   float64 `yaml:"max_trade_loss_pct"`
	MaxOpenPositions                  int     `yaml:"max_open_positions"`
	MaxSpreadBps                      float64 `yaml:"max_spread_bps"`
	MaxConsecutiveLosses              int     `yaml:"max_consecutive_losses"`
	CooldownAfterLossMinutes          int     `yaml:"cooldown_after_loss_minutes"`
	ForceFlatBeforeMarketCloseMinutes int     `yaml:"force_flat_before_market_close_minutes"`
	HeartbeatTimeoutMinutes           int     `yaml:"heartbeat_timeout_minutes"`
}

// LoadRisk reads and parses the risk configuration YAML file at path
// (conventionally DefaultRiskPath) into a RiskConfig.
func LoadRisk(path string) (*RiskConfig, error) {
	return loadYAMLFile[RiskConfig](path)
}

// LoadRiskBytes parses data (conventionally an embedded copy of
// config/risk.yaml, github.com/ousiassllc/pitha-trador/config's
// configdefaults.DefaultRiskYAML) as a RiskConfig. internal/bootstrap
// falls back to this when no risk.yaml is found on disk (explicit path,
// PITHA_RISK_PATH, nor next to the running executable) so a distributed
// .exe with no accompanying config/ directory still starts.
func LoadRiskBytes(data []byte) (*RiskConfig, error) {
	return loadYAMLBytes[RiskConfig](data, "(embedded default)")
}
