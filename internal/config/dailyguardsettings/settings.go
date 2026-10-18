package dailyguardsettings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
)

// Settings are the stored runtime_settings rows of Keys. A nil field means
// that key has no (valid) stored row.
type Settings struct {
	CapitalJPY            *float64
	LossLimitPct          *float64
	ProfitLockTriggerPct  *float64
	LossLimitJPY          *float64
	ProfitLockTriggerJPY  *float64
	ProfitLockGivebackPct *float64
	MaxTrades             *int
}

// MaxCapitalJPY is the largest operating capital Apply accepts (1 兆円),
// far above any real account and far below what could overflow the
// percentage arithmetic.
const MaxCapitalJPY = 1e12

// Apply writes jsonValue - a runtime_settings.value, a JSON number - into
// the field named by key after checking its range: 0 or greater, the
// operating capital at most MaxCapitalJPY, and for the percentages
// (KeyLossLimitPct, KeyProfitLockTriggerPct, KeyProfitLockGivebackPct) at
// most 100. s is left untouched on error.
func Apply(s *Settings, key, jsonValue string) error {
	var n float64
	if err := json.Unmarshal([]byte(jsonValue), &n); err != nil {
		return fmt.Errorf("dailyguardsettings: decode runtime setting %s=%q as a number: %w", key, jsonValue, err)
	}
	if math.IsNaN(n) || math.IsInf(n, 0) || n < 0 {
		return fmt.Errorf("dailyguardsettings: runtime setting %s=%q must be 0 or greater", key, jsonValue)
	}
	var target **float64
	switch key {
	case KeyOperatingCapitalJPY:
		target = &s.CapitalJPY
	case KeyLossLimitJPY:
		target = &s.LossLimitJPY
	case KeyProfitLockTriggerJPY:
		target = &s.ProfitLockTriggerJPY
	case KeyLossLimitPct:
		target = &s.LossLimitPct
	case KeyProfitLockTriggerPct:
		target = &s.ProfitLockTriggerPct
	case KeyProfitLockGivebackPct:
		target = &s.ProfitLockGivebackPct
	case KeyMaxTrades:
		if n != math.Trunc(n) || n > math.MaxInt32 {
			return fmt.Errorf("dailyguardsettings: runtime setting %s=%q must be a whole number", key, jsonValue)
		}
		trades := int(n)
		s.MaxTrades = &trades
		return nil
	default:
		return fmt.Errorf("dailyguardsettings: %q is not a daily guard setting key", key)
	}
	if isPercentKey(key) && n > 100 {
		return fmt.Errorf("dailyguardsettings: runtime setting %s=%q must be 0〜100", key, jsonValue)
	}
	if key == KeyOperatingCapitalJPY && n > MaxCapitalJPY {
		return fmt.Errorf("dailyguardsettings: runtime setting %s=%q: 運用資金は 1 兆円（1000000000000 円）以下で指定してください", key, jsonValue)
	}
	*target = &n
	return nil
}

func isPercentKey(key string) bool {
	return key == KeyLossLimitPct || key == KeyProfitLockTriggerPct || key == KeyProfitLockGivebackPct
}

// Store is the runtime_settings read access Load needs;
// *system.RuntimeSettingsRepository implements it.
type Store interface {
	Get(ctx context.Context, key string) (value string, ok bool, err error)
}

// LoadSettings reads every stored row. A row that no longer parses (the
// Settings screen stores only valid ones) is logged and treated as not
// stored, so a damaged row cannot stop trading; only a store read failure is
// an error.
func LoadSettings(ctx context.Context, store Store) (Settings, error) {
	var s Settings
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
		if err := Apply(&s, key, raw); err != nil {
			slog.Warn("dailyguardsettings: ignoring invalid stored threshold; rule disabled", "error", err)
		}
	}
	if err := errors.Join(errs...); err != nil {
		return Settings{}, err
	}
	return s, nil
}

// Load returns the effective limits (LoadResolved's Limits): each stored row
// over its default (disabled). Only a store read failure is an error.
func Load(ctx context.Context, store Store) (Limits, error) {
	r, err := LoadResolved(ctx, store)
	return r.Limits, err
}

// LoadResolved is Load plus how each yen threshold was derived.
func LoadResolved(ctx context.Context, store Store) (Resolved, error) {
	s, err := LoadSettings(ctx, store)
	if err != nil {
		return Resolved{}, err
	}
	return Resolve(s), nil
}
