// Package dailyguardsettings holds the Settings-screen thresholds of the
// day-trading entry-halt rules (issue #753, functional.md FR-RISK-8): a
// daily loss limit, a profit lock and a maximum number of trades per day.
// They live in runtime_settings (docs/architecture/er.md §runtime_settings)
// under Keys; there is no yaml or environment source. A threshold of 0 - or
// a key with no stored row - disables that rule.
//
// The two yen thresholds can be given directly (KeyLossLimitJPY,
// KeyProfitLockTriggerJPY) or derived from an operating capital and a
// percentage (KeyOperatingCapitalJPY with KeyLossLimitPct /
// KeyProfitLockTriggerPct, issue #782); Resolve turns the stored rows into
// the Limits the risk engine reads. The risk engine only ever sees Limits.
package dailyguardsettings

import "slices"

// runtime_settings keys; every value is a JSON number (KeyMaxTrades an
// integer). KeyLossLimitJPY and KeyProfitLockTriggerJPY are the direct-yen
// (compat) thresholds; KeyOperatingCapitalJPY (yen, 0 = unset),
// KeyLossLimitPct and KeyProfitLockTriggerPct (0〜100, 0 = not used) derive
// the same two thresholds from the capital. See Limits and Resolve.
const (
	KeyOperatingCapitalJPY   = "risk.operating_capital_jpy"
	KeyLossLimitPct          = "risk.daily_loss_limit_pct"
	KeyProfitLockTriggerPct  = "risk.profit_lock.trigger_pct"
	KeyLossLimitJPY          = "risk.daily_loss_limit_jpy"
	KeyProfitLockTriggerJPY  = "risk.profit_lock.trigger_jpy"
	KeyProfitLockGivebackPct = "risk.profit_lock.giveback_pct"
	KeyMaxTrades             = "risk.max_daily_trades"
)

// Limits are the effective thresholds. The zero value disables every rule.
type Limits struct {
	// LossLimitJPY stops new entries once the day's realized + unrealized
	// P&L has fallen to -LossLimitJPY (yen, > 0).
	LossLimitJPY float64
	// ProfitLockTriggerJPY arms the profit lock once the day's P&L has
	// reached it (yen, > 0); ProfitLockGivebackPct (0 < pct <= 100) then
	// stops new entries when the P&L has fallen that percentage of the way
	// back from the day's peak. The lock needs both to be set.
	ProfitLockTriggerJPY  float64
	ProfitLockGivebackPct float64
	// MaxTrades stops new entries once that many positions were opened
	// today (>= 1).
	MaxTrades int
}

// Keys returns every runtime_settings key Apply accepts.
func Keys() []string {
	return []string{
		KeyOperatingCapitalJPY, KeyLossLimitPct, KeyProfitLockTriggerPct, KeyProfitLockGivebackPct, KeyMaxTrades,
		KeyLossLimitJPY, KeyProfitLockTriggerJPY,
	}
}

// IsKey reports whether key is one of Keys.
func IsKey(key string) bool {
	return slices.Contains(Keys(), key)
}

// IsIntegerKey reports whether key's value is a whole number (the others
// are decimals).
func IsIntegerKey(key string) bool { return key == KeyMaxTrades }

// LossLimitEnabled reports whether the daily loss limit applies.
func (l Limits) LossLimitEnabled() bool { return l.LossLimitJPY > 0 }

// ProfitLockEnabled reports whether the profit lock applies (both of its
// thresholds are set).
func (l Limits) ProfitLockEnabled() bool {
	return l.ProfitLockTriggerJPY > 0 && l.ProfitLockGivebackPct > 0
}

// MaxTradesEnabled reports whether the daily trade-count cap applies.
func (l Limits) MaxTradesEnabled() bool { return l.MaxTrades > 0 }

// AnyEnabled reports whether at least one rule applies.
func (l Limits) AnyEnabled() bool {
	return l.LossLimitEnabled() || l.ProfitLockEnabled() || l.MaxTradesEnabled()
}
