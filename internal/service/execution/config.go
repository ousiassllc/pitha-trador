package execution

import (
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/service/risk/sizing"
)

// Config is Engine's Entry/Exit rule set. NewEngine only fills in Now
// when nil; every numeric field is used exactly as given - a zero value
// explicitly disables that one exit condition (mirrors
// internal/service/backtest.ExitRule), rather than falling back to a
// default. A caller that wants FR-EXIT-2's initial values starts from
// DefaultConfig() or ConfigFromRiskLimits, not a zero-valued Config{}.
type Config struct {
	// PreferLimit is FR-ENTRY-2's "実売買へ移行する場合は原則として指値
	// を優先する" toggle: when true, Enter defaults a request with no
	// explicit OrderType to OrderTypeLimit instead of OrderTypeMarket.
	// Defaults to false (Paper Trading's current default is market,
	// per FR-ENTRY-1 listing 成行 first).
	PreferLimit bool

	// StopLossPct/TakeProfitPct/TrailingStopPct/MaxHoldingMinutes are
	// FR-EXIT-2's initial values (stop_loss_pct=0.6, take_profit_pct=1.2,
	// trailing_stop_pct=0.5, max_holding_minutes=20). A zero value
	// disables that one exit condition (mirrors
	// internal/service/backtest.ExitRule).
	StopLossPct       float64
	TakeProfitPct     float64
	TrailingStopPct   float64
	MaxHoldingMinutes int

	// MinContinuationProbability is the continuation_probability低下
	// exit condition's threshold (FR-EXIT-1). Defaults to 0.60,
	// config/strategy.yaml's policy.long/short.min_continuation_probability
	// entry threshold - once continuation_probability drops back below
	// the level Policy Engine required to enter, Execution treats the
	// original setup as no longer intact.
	MinContinuationProbability float64

	// CooldownAfterLossMinutes/ForceFlatBeforeMarketCloseMinutes mirror
	// config.RiskLimits' same-named fields (config/risk.yaml, Paper
	// initial values 5 / 10) for the force-flat-before-close exit
	// condition and this package's own per-symbol cooldown_until state
	// (functional.md §4.9), independent of internal/service/risk's own
	// portfolio-wide cooldown gate (engine.go's doc comment).
	CooldownAfterLossMinutes          int
	ForceFlatBeforeMarketCloseMinutes int

	// Now defaults to time.Now. Tests override it for deterministic
	// max-holding/force-flat/cooldown checks.
	Now func() time.Time
}

// DefaultConfig returns FR-EXIT-2's initial values
// (stop_loss_pct=0.6, take_profit_pct=1.2, trailing_stop_pct=0.5,
// max_holding_minutes=20) plus config/risk.yaml's Paper
// cooldown_after_loss_minutes/force_flat_before_market_close_minutes
// (5/10).
func DefaultConfig() Config {
	return Config{
		StopLossPct:                       sizing.DefaultStopLossPct, // Risk Engine sizes positions against this same stop
		TakeProfitPct:                     1.2,
		TrailingStopPct:                   0.5,
		MaxHoldingMinutes:                 20,
		MinContinuationProbability:        0.60,
		CooldownAfterLossMinutes:          5,
		ForceFlatBeforeMarketCloseMinutes: 10,
	}
}

// ConfigFromRiskLimits returns DefaultConfig() with
// CooldownAfterLossMinutes/ForceFlatBeforeMarketCloseMinutes overridden
// from limits (config/risk.yaml's Paper or Live section), for a caller
// wiring Engine to the same config.RiskLimits it passed to
// risk.NewEngine.
func ConfigFromRiskLimits(limits config.RiskLimits) Config {
	cfg := DefaultConfig()
	cfg.CooldownAfterLossMinutes = limits.CooldownAfterLossMinutes
	cfg.ForceFlatBeforeMarketCloseMinutes = limits.ForceFlatBeforeMarketCloseMinutes
	return cfg
}

// withDefaults fills in Now when nil; every other field is used exactly
// as the caller set it (see Config's doc comment for why zero is not
// coerced to DefaultConfig()'s values here).
func (c Config) withDefaults() Config {
	if c.Now == nil {
		c.Now = time.Now
	}
	return c
}
