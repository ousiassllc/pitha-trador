// Package jquantssettings holds the Settings-screen keys of the J-Quants read
// adapter (issue #1093): the switch, the plan flags (which endpoints are
// subscribed), the daily request cap, how far back the backfill reaches, and the
// request to delete the stored J-Quants data (a cancelled subscription's
// clean-up).
//
// Every key lives in runtime_settings (docs/architecture/er.md §runtime_settings);
// there is no yaml or environment source. Everything is OFF by default: while
// nothing is stored (or the stored value is damaged) the adapter makes no
// external request and writes nothing. None of the keys is on the Paper results
// MCP's settings_set whitelist (mcpwritesettings): the adapter calls a paid API
// and the purge deletes data, so they are changed on the Settings screen only.
//
// The API key is not here: it is the secret JQUANTS_API_KEY (internal/config).
package jquantssettings

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"slices"
)

// Keys of the runtime_settings rows.
const (
	// KeyEnabled is the switch: the JSON integer 1 (fetch) or 0 (do not).
	KeyEnabled = "jquants.enabled"
	// KeyDailyRequestCap is the most requests (pages included) sent to J-Quants
	// per JST day: a JSON integer, MinDailyRequestCap..MaxDailyRequestCap.
	KeyDailyRequestCap = "jquants.daily_request_cap"
	// KeyPlanDaily / KeyPlanMinute say whether the daily-bars / the minute-bars
	// endpoint is subscribed (the JSON integer 1 or 0). An endpoint that is not
	// subscribed is never called.
	KeyPlanDaily  = "jquants.plan_daily"
	KeyPlanMinute = "jquants.plan_minute"
	// KeyBackfillDays is how many JST days back from yesterday the backfill
	// looks for days it has not fetched: a JSON integer.
	KeyBackfillDays = "jquants.backfill_days"
	// KeyPurgeRequested asks the daily task to delete every stored J-Quants row
	// (the JSON integer 1); the task resets it to nothing when done. Allowed
	// while the switch is off (the clean-up after cancelling).
	KeyPurgeRequested = "jquants.purge_requested"
)

// Defaults while nothing is stored.
const (
	DefaultEnabled       = false
	DefaultPlan          = false
	DefaultPurge         = false
	DefaultDailyRequests = 200
	DefaultBackfillDays  = 30
)

// Bounds of the integer keys.
const (
	MinDailyRequestCap = 1
	MaxDailyRequestCap = 5000
	MinBackfillDays    = 1
	// MaxBackfillDays is the 5 years of daily bars of the Light plan; the minute
	// bars reach back 2 years only (MaxMinuteDays).
	MaxBackfillDays = 1830
	MaxMinuteDays   = 730
)

var keys = []string{KeyEnabled, KeyDailyRequestCap, KeyPlanDaily, KeyPlanMinute, KeyBackfillDays, KeyPurgeRequested}

// Keys returns every runtime_settings key of this package.
func Keys() []string { return slices.Clone(keys) }

// IsKey reports whether key is one of Keys.
func IsKey(key string) bool { return slices.Contains(keys, key) }

// IsSwitchKey reports whether key holds the JSON integer 0 or 1.
func IsSwitchKey(key string) bool {
	return key == KeyEnabled || key == KeyPlanDaily || key == KeyPlanMinute || key == KeyPurgeRequested
}

// DefaultText is what applies to key while nothing is stored, as the Settings
// screen shows it.
func DefaultText(key string) string {
	switch key {
	case KeyDailyRequestCap:
		return fmt.Sprint(DefaultDailyRequests)
	case KeyBackfillDays:
		return fmt.Sprint(DefaultBackfillDays)
	}
	return "0"
}

// Parse decodes and range-checks the runtime_settings value of key. Switches are
// the JSON integer 0 or 1; the others are whole numbers within their bounds.
func Parse(key, jsonValue string) (int, error) {
	var n *float64 // a pointer, so JSON null is told from 0
	if err := json.Unmarshal([]byte(jsonValue), &n); err != nil {
		return 0, fmt.Errorf("jquantssettings: decode runtime setting %s=%q as a number: %w", key, jsonValue, err)
	}
	if n == nil || math.IsNaN(*n) || *n != math.Trunc(*n) {
		return 0, fmt.Errorf("jquantssettings: runtime setting %s=%q must be a whole number", key, jsonValue)
	}
	lo, hi := bounds(key)
	if *n < float64(lo) || *n > float64(hi) {
		if IsSwitchKey(key) {
			return 0, fmt.Errorf("jquantssettings: runtime setting %s=%q must be 0（無効）または 1（有効）", key, jsonValue)
		}
		return 0, fmt.Errorf("jquantssettings: runtime setting %s=%q must be %d〜%d", key, jsonValue, lo, hi)
	}
	return int(*n), nil
}

func bounds(key string) (lo, hi int) {
	switch key {
	case KeyDailyRequestCap:
		return MinDailyRequestCap, MaxDailyRequestCap
	case KeyBackfillDays:
		return MinBackfillDays, MaxBackfillDays
	}
	return 0, 1
}

// Store is the runtime_settings access Load needs; *system.RuntimeSettingsRepository
// implements it.
type Store interface {
	Get(ctx context.Context, key string) (value string, ok bool, err error)
}

// Settings is the effective state of every key.
type Settings struct {
	Enabled         bool
	PlanDaily       bool
	PlanMinute      bool
	PurgeRequested  bool
	DailyRequestCap int
	BackfillDays    int
}

// Default is the state while nothing is stored: off, nothing subscribed.
func Default() Settings {
	return Settings{DailyRequestCap: DefaultDailyRequests, BackfillDays: DefaultBackfillDays}
}

// Load reads the effective settings. A key with nothing stored, or a stored row
// that does not parse (the Settings screen stores only valid ones), is logged if
// damaged and counts as its default, so a fault can neither switch the adapter
// on, mark an endpoint subscribed, nor raise the cap. Only a failed store read is
// an error: the caller must not guess then.
func Load(ctx context.Context, store Store) (Settings, error) {
	s := Default()
	values := map[string]int{}
	for _, key := range keys {
		raw, ok, err := store.Get(ctx, key)
		if err != nil {
			return Settings{}, fmt.Errorf("jquantssettings: read %s: %w", key, err)
		}
		if !ok {
			continue
		}
		n, perr := Parse(key, raw)
		if perr != nil {
			slog.Warn("jquantssettings: ignoring invalid stored setting; its default applies", "key", key, "error", perr)
			continue
		}
		values[key] = n
	}
	s.Enabled = values[KeyEnabled] == 1
	s.PlanDaily = values[KeyPlanDaily] == 1
	s.PlanMinute = values[KeyPlanMinute] == 1
	s.PurgeRequested = values[KeyPurgeRequested] == 1
	if n, ok := values[KeyDailyRequestCap]; ok {
		s.DailyRequestCap = n
	}
	if n, ok := values[KeyBackfillDays]; ok {
		s.BackfillDays = n
	}
	return s, nil
}
