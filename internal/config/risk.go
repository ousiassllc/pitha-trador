package config

import "log/slog"

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
//
// InitialCapital is the account-equity baseline (JPY) every
// "percentage of account equity" limit (max_position_per_symbol_pct,
// max_total_exposure_pct, max_daily_loss_pct, max_trade_loss_pct) is
// measured against and Execution's position sizing is derived from
// (functional.md §4.7/§4.8). Paper: the simulated capital; Live: the
// capital allocated to this system. A value <= 0 means "unset".
type RiskLimits struct {
	InitialCapital                    float64 `yaml:"initial_capital"`
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

// DefaultPaperInitialCapital is the Paper Trading equity baseline (JPY)
// used when config/risk.yaml's paper.initial_capital is unset (an
// operator's older on-disk risk.yaml); it equals the shipped
// config/risk.yaml value.
const DefaultPaperInitialCapital = 30_000_000

// LoadRisk reads and parses the risk configuration YAML file at path
// (conventionally DefaultRiskPath) into a RiskConfig.
func LoadRisk(path string) (*RiskConfig, error) {
	cfg, err := loadYAMLFile[RiskConfig](path)
	return withPaperCapitalDefault(cfg, err)
}

// LoadRiskBytes parses data (conventionally an embedded copy of
// config/risk.yaml, github.com/ousiassllc/pitha-trador/config's
// configdefaults.DefaultRiskYAML) as a RiskConfig. internal/bootstrap
// falls back to this when no risk.yaml is found on disk (explicit path,
// PITHA_RISK_PATH, nor next to the running executable) so a distributed
// .exe with no accompanying config/ directory still starts.
func LoadRiskBytes(data []byte) (*RiskConfig, error) {
	cfg, err := loadYAMLBytes[RiskConfig](data, "(embedded default)")
	return withPaperCapitalDefault(cfg, err)
}

// withPaperCapitalDefault fills DefaultPaperInitialCapital into a
// risk.yaml (e.g. an operator's older on-disk copy) that predates
// paper.initial_capital, warning so the operator can set it explicitly.
// Live gets no default: with no capital configured Risk Engine rejects
// every new trade (fail closed) instead of sizing against a made-up
// baseline.
func withPaperCapitalDefault(cfg *RiskConfig, err error) (*RiskConfig, error) {
	if err != nil || cfg.Paper.InitialCapital > 0 {
		return cfg, err
	}
	slog.Warn("config: paper.initial_capital is not set in risk.yaml; using default",
		"default_jpy", DefaultPaperInitialCapital)
	cfg.Paper.InitialCapital = DefaultPaperInitialCapital
	return cfg, nil
}
