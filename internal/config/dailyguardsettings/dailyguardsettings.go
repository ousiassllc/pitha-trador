// Package dailyguardsettings holds the Settings-screen thresholds of the
// day-trading entry-halt rules (issue #753, functional.md FR-RISK-8): a
// daily loss limit, a profit lock and a maximum number of trades per day.
// They live in runtime_settings (docs/architecture/er.md §runtime_settings)
// under Keys; there is no yaml or environment source. A threshold of 0 - or
// a key with no stored row - disables that rule.
package dailyguardsettings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
)

// runtime_settings keys. The first, second and fourth values are JSON
// numbers (the fourth an integer); see Limits for their meaning.
const (
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
	return []string{KeyLossLimitJPY, KeyProfitLockTriggerJPY, KeyProfitLockGivebackPct, KeyMaxTrades}
}

// IsKey reports whether key is one of Keys.
func IsKey(key string) bool {
	switch key {
	case KeyLossLimitJPY, KeyProfitLockTriggerJPY, KeyProfitLockGivebackPct, KeyMaxTrades:
		return true
	}
	return false
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

// Apply writes jsonValue - a runtime_settings.value, a JSON number - into
// the threshold named by key after checking its range.
func Apply(l *Limits, key, jsonValue string) error {
	var n float64
	if err := json.Unmarshal([]byte(jsonValue), &n); err != nil {
		return fmt.Errorf("dailyguardsettings: decode runtime setting %s=%q as a number: %w", key, jsonValue, err)
	}
	if math.IsNaN(n) || math.IsInf(n, 0) || n < 0 {
		return fmt.Errorf("dailyguardsettings: runtime setting %s=%q must be 0 or greater", key, jsonValue)
	}
	switch key {
	case KeyLossLimitJPY:
		l.LossLimitJPY = n
	case KeyProfitLockTriggerJPY:
		l.ProfitLockTriggerJPY = n
	case KeyProfitLockGivebackPct:
		if n > 100 {
			return fmt.Errorf("dailyguardsettings: runtime setting %s=%q must be 0〜100", key, jsonValue)
		}
		l.ProfitLockGivebackPct = n
	case KeyMaxTrades:
		if n != math.Trunc(n) || n > math.MaxInt32 {
			return fmt.Errorf("dailyguardsettings: runtime setting %s=%q must be a whole number", key, jsonValue)
		}
		l.MaxTrades = int(n)
	default:
		return fmt.Errorf("dailyguardsettings: %q is not a daily guard setting key", key)
	}
	return nil
}

// Store is the runtime_settings read access Load needs;
// *system.RuntimeSettingsRepository implements it.
type Store interface {
	Get(ctx context.Context, key string) (value string, ok bool, err error)
}

// Load returns the effective limits: each stored row over its default
// (disabled). A stored row that no longer parses (the Settings screen stores
// only valid ones) is logged and leaves that rule disabled, so a damaged
// row cannot stop trading; only a store read failure is an error.
func Load(ctx context.Context, store Store) (Limits, error) {
	var l Limits
	var errs []error
	for _, key := range Keys() {
		raw, ok, err := store.Get(ctx, key)
		if err != nil {
			errs = append(errs, fmt.Errorf("dailyguardsettings: read %s: %w", key, err))
			continue
		}
		if !ok {
			continue
		}
		if err := Apply(&l, key, raw); err != nil {
			slog.Warn("dailyguardsettings: ignoring invalid stored threshold; rule disabled", "error", err)
		}
	}
	if err := errors.Join(errs...); err != nil {
		return Limits{}, err
	}
	return l, nil
}
