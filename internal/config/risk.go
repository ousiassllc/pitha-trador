package config

import (
	"errors"
	"fmt"
	"log/slog"
)

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
//
// MaxSameDirectionPositions caps the open positions pointing the same way
// (LONG or SHORT); MarketAdverseReturn5mPct is the 5-minute market-index
// return (percent) against a held direction at which adding to it is
// rejected (functional.md §4.7 FR-RISK-1 correlation / market-adverse gate).
type RiskLimits struct {
	InitialCapital                    float64 `yaml:"initial_capital"`
	MaxPositionPerSymbolPct           float64 `yaml:"max_position_per_symbol_pct"`
	MaxTotalExposurePct               float64 `yaml:"max_total_exposure_pct"`
	MaxDailyLossPct                   float64 `yaml:"max_daily_loss_pct"`
	MaxTradeLossPct                   float64 `yaml:"max_trade_loss_pct"`
	MaxOpenPositions                  int     `yaml:"max_open_positions"`
	MaxSameDirectionPositions         int     `yaml:"max_same_direction_positions"`
	MarketAdverseReturn5mPct          float64 `yaml:"market_adverse_return_5m_pct"`
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
// (conventionally DefaultRiskPath) into a RiskConfig, failing with an
// error naming the offending key when a limit is out of range (see
// RiskConfig.Validate).
func LoadRisk(path string) (*RiskConfig, error) {
	cfg, err := loadYAMLFile[RiskConfig](path)
	return finishRisk(cfg, err)
}

// LoadRiskBytes parses data (conventionally an embedded copy of
// config/risk.yaml, github.com/ousiassllc/pitha-trador/config's
// configdefaults.DefaultRiskYAML) as a RiskConfig. internal/bootstrap
// falls back to this when no risk.yaml is found on disk (explicit path,
// PITHA_RISK_PATH, nor next to the running executable) so a distributed
// .exe with no accompanying config/ directory still starts.
func LoadRiskBytes(data []byte) (*RiskConfig, error) {
	cfg, err := loadYAMLBytes[RiskConfig](data, "(embedded default)")
	return finishRisk(cfg, err)
}

// finishRisk applies LoadRisk/LoadRiskBytes' shared post-parse steps: the
// Paper initial_capital default, then limit validation.
func finishRisk(cfg *RiskConfig, err error) (*RiskConfig, error) {
	cfg, err = withPaperCapitalDefault(cfg, err)
	if err != nil {
		return cfg, err
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Validate rejects limits that would make Risk Engine misbehave. A key
// omitted from risk.yaml (or a typo'd one) decodes as 0, and a 0 limit is
// not "unlimited": max_daily_loss_pct=0 trips daily_loss_limit and
// max_consecutive_losses=0 trips consecutive_losses with no loss at all,
// re-firing the Kill Switch after every manual resume
// (functional/components-pipeline.md §4.7 FR-RISK-1/2).
//
// Paper is always validated. Live is validated only when the live section
// defines at least one value: like initial_capital, a risk.yaml with no
// live section simply isn't set up for Live (no Live limits to misapply).
// Every violation is reported at once.
func (c *RiskConfig) Validate() error {
	errs := c.Paper.validate("paper")
	if c.Live != (RiskLimits{}) {
		errs = append(errs, c.Live.validate("live")...)
	}
	if len(errs) == 0 {
		return nil
	}
	return fmt.Errorf("config: invalid risk.yaml: %w", errors.Join(errs...))
}

// validate checks one mode's limits (mode is "paper" or "live", used as
// the error-message key prefix). initial_capital is deliberately not
// checked here: <= 0 is handled by the Paper default / Live fail-closed
// rule documented on RiskLimits.
func (l RiskLimits) validate(mode string) []error {
	var errs []error
	positive := func(key string, v float64) {
		if !(v > 0) { // also rejects NaN
			errs = append(errs, fmt.Errorf("%s.%s must be > 0 (got %v)", mode, key, v))
		}
	}
	atLeastOne := func(key string, v int) {
		if v < 1 {
			errs = append(errs, fmt.Errorf("%s.%s must be >= 1 (got %d)", mode, key, v))
		}
	}
	nonNegative := func(key string, v int) {
		if v < 0 {
			errs = append(errs, fmt.Errorf("%s.%s must be >= 0 (got %d)", mode, key, v))
		}
	}
	positive("max_position_per_symbol_pct", l.MaxPositionPerSymbolPct)
	positive("max_total_exposure_pct", l.MaxTotalExposurePct)
	positive("max_daily_loss_pct", l.MaxDailyLossPct)
	positive("max_trade_loss_pct", l.MaxTradeLossPct)
	positive("max_spread_bps", l.MaxSpreadBps)
	atLeastOne("max_open_positions", l.MaxOpenPositions)
	atLeastOne("max_same_direction_positions", l.MaxSameDirectionPositions)
	positive("market_adverse_return_5m_pct", l.MarketAdverseReturn5mPct)
	atLeastOne("max_consecutive_losses", l.MaxConsecutiveLosses)
	nonNegative("cooldown_after_loss_minutes", l.CooldownAfterLossMinutes)
	nonNegative("force_flat_before_market_close_minutes", l.ForceFlatBeforeMarketCloseMinutes)
	nonNegative("heartbeat_timeout_minutes", l.HeartbeatTimeoutMinutes)
	return errs
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
