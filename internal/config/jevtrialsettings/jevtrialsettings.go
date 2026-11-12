// Package jevtrialsettings defines the runtime_settings keys of the
// model-comparison trial (issue #1059): whether the trial runs (Paper only,
// never used for trading), which arms it asks (a JSON array), its overall daily
// cost cap and, per arm, the records of why it stopped for a day.
//
// The trial is separate from the Jev shadow (jevshadowsettings, `jev.shadow.*`):
// no key here starts with `jev.shadow.`, and the shadow's code never reads
// these. Everything is OFF/empty by default: nothing stored means the trial
// does nothing and makes no external call.
//
// The keys live in runtime_settings and are read by the application
// (jev/trial.Trial, the Settings screen) and by the Paper results MCP, which
// share the database but not memory. None of them is on the MCP's settings_set
// whitelist (mcpwritesettings): the trial costs money, so turning it on is done
// on the Settings screen only. The overall cap and the arms likewise.
package jevtrialsettings

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/ousiassllc/pitha-trador/internal/config/jevshadowsettings"
)

// Keys of the runtime_settings rows.
const (
	// KeyEnabled is the trial switch: the JSON integer 1 (on) or 0 (off). OFF
	// while nothing is stored or the stored value is damaged.
	KeyEnabled = "jev.trial.enabled"
	// KeyArms is the arms (a JSON array of Arm). Nothing stored: no arm.
	KeyArms = "jev.trial.arms"
	// KeyDailyCostCapUSD is the overall daily cost cap in USD across all arms
	// (a JSON number). Nothing stored means DefaultDailyCostCapUSD.
	KeyDailyCostCapUSD = "jev.trial.daily_cost_cap_usd"
	// KeyHalt records a stop of the whole trial for the day (the overall cap, or
	// HTTP 402: the account is out of credit). A JSON Halt.
	KeyHalt = "jev.trial.halt"

	// PrefixHaltArm, PrefixFailuresArm and PrefixThrottleArm start the per-arm
	// keys: the day's stop of one arm (Halt), its failed-call counter (Failures)
	// and its 429/529 stop (Throttle). The suffix is the arm id.
	PrefixHaltArm     = "jev.trial.halt."
	PrefixFailuresArm = "jev.trial.failures."
	PrefixThrottleArm = "jev.trial.throttle."
)

// KeyPrefix starts every key of this package.
const KeyPrefix = "jev.trial."

// Keys returns the keys that exist independently of an arm.
func Keys() []string { return []string{KeyEnabled, KeyArms, KeyDailyCostCapUSD, KeyHalt} }

// HaltKey, FailuresKey and ThrottleKey are arm's per-arm keys.
func HaltKey(arm string) string     { return PrefixHaltArm + arm }
func FailuresKey(arm string) string { return PrefixFailuresArm + arm }
func ThrottleKey(arm string) string { return PrefixThrottleArm + arm }

// IsKey reports whether key is one of this package's keys (an arm-less key or a
// per-arm one).
func IsKey(key string) bool { return strings.HasPrefix(key, KeyPrefix) }

// DefaultEnabled is the trial switch while nothing is stored.
const DefaultEnabled = false

// DefaultDailyCostCapUSD is the overall daily cap while nothing is stored, in
// USD per JST day (the same default as the shadow's cap; validated by
// jevshadowsettings.ValidateCap).
const DefaultDailyCostCapUSD = 0.10

// DefaultArmDailyCostCapUSD is an arm's daily cap when its configuration does
// not give one. It is deliberately small: the per-call cost of the models is
// not measured yet.
const DefaultArmDailyCostCapUSD = 0.05

// Reused from the shadow: the failed-call cap of a day, the cooldown after a
// 429/529 and the least time between two calls of one arm.
const (
	DailyFailureCap  = jevshadowsettings.DailyFailureCap
	ThrottleCooldown = jevshadowsettings.ThrottleCooldown
	MinCallInterval  = jevshadowsettings.MinCallInterval
)

// ParseEnabled decodes the runtime_settings value of KeyEnabled (the JSON integer
// 0 or 1).
func ParseEnabled(jsonValue string) (bool, error) {
	var n *float64
	if err := json.Unmarshal([]byte(jsonValue), &n); err != nil {
		return false, fmt.Errorf("jevtrialsettings: decode runtime setting %s=%q as a number: %w", KeyEnabled, jsonValue, err)
	}
	if n == nil || (*n != 0 && *n != 1) {
		return false, fmt.Errorf("jevtrialsettings: runtime setting %s=%q must be 0 or 1", KeyEnabled, jsonValue)
	}
	return *n == 1, nil
}

// Switch renders a switch state as stored: "1" or "0".
func Switch(enabled bool) string {
	if enabled {
		return "1"
	}
	return "0"
}

// ValidateCap checks a daily cost cap (overall or an arm's): the same rule as the
// shadow's (finite, above 0, at most jevshadowsettings.MaxDailyCostCapUSD).
func ValidateCap(usd float64) error {
	if err := jevshadowsettings.ValidateCap(usd); err != nil {
		return fmt.Errorf("jevtrialsettings: the daily cost cap must be above 0 and at most %g USD", jevshadowsettings.MaxDailyCostCapUSD)
	}
	return nil
}

// ParseCap decodes the runtime_settings value of KeyDailyCostCapUSD.
func ParseCap(jsonValue string) (float64, error) {
	var n float64
	if err := json.Unmarshal([]byte(jsonValue), &n); err != nil {
		return 0, fmt.Errorf("jevtrialsettings: decode runtime setting %s=%q as a number: %w", KeyDailyCostCapUSD, jsonValue, err)
	}
	if err := ValidateCap(n); err != nil {
		return 0, fmt.Errorf("jevtrialsettings: runtime setting %s=%q: %w", KeyDailyCostCapUSD, jsonValue, err)
	}
	return n, nil
}

// Store is the runtime_settings read access Load needs;
// *system.RuntimeSettingsRepository implements it.
type Store interface {
	Get(ctx context.Context, key string) (value string, ok bool, err error)
}

// Settings is the effective state of the arm-less keys.
type Settings struct {
	// Enabled is the trial switch.
	Enabled bool
	// Arms are the configured arms, nil for none (also when the stored value
	// does not parse: ArmsError then says why).
	Arms      []Arm
	ArmsError string
	// CapUSD is the overall daily cap; CapIsDefault is true while it is
	// DefaultDailyCostCapUSD.
	CapUSD       float64
	CapIsDefault bool
	// Halt is the recorded stop of the whole trial, nil for none.
	Halt *Halt
}

// Load reads the arm-less keys. A stored row that no longer parses, or a failed
// read, is logged and falls back to the safe side: the trial off, no arms, the
// default cap, no halt record. A fault can therefore never turn the trial on.
func Load(ctx context.Context, store Store) Settings {
	s := Settings{CapUSD: DefaultDailyCostCapUSD, CapIsDefault: true}
	if raw, ok := read(ctx, store, KeyEnabled); ok {
		if on, err := ParseEnabled(raw); err == nil {
			s.Enabled = on
		} else {
			slog.Warn("jevtrialsettings: treating a damaged trial switch as off", "error", err)
		}
	}
	if raw, ok := read(ctx, store, KeyArms); ok {
		arms, err := ParseArms(raw)
		if err != nil {
			s.ArmsError = err.Error()
			slog.Warn("jevtrialsettings: ignoring the stored arms", "error", err)
		} else {
			s.Arms = arms
		}
	}
	if raw, ok := read(ctx, store, KeyDailyCostCapUSD); ok {
		if c, err := ParseCap(raw); err == nil {
			s.CapUSD, s.CapIsDefault = c, false
		} else {
			slog.Warn("jevtrialsettings: using the default overall daily cost cap", "error", err)
		}
	}
	if raw, ok := read(ctx, store, KeyHalt); ok {
		if h, err := ParseHalt(KeyHalt, raw); err == nil {
			s.Halt = &h
		} else {
			slog.Warn("jevtrialsettings: ignoring the stored halt record", "error", err)
		}
	}
	return s
}

// ArmState is the stored per-arm state of one arm.
type ArmState struct {
	Halt     *Halt
	Failures *Failures
	Throttle *Throttle
}

// LoadArm reads arm's per-arm keys with the same fallbacks as Load.
func LoadArm(ctx context.Context, store Store, arm string) ArmState {
	var s ArmState
	if raw, ok := read(ctx, store, HaltKey(arm)); ok {
		if h, err := ParseHalt(HaltKey(arm), raw); err == nil {
			s.Halt = &h
		} else {
			slog.Warn("jevtrialsettings: ignoring the stored arm halt record", "arm", arm, "error", err)
		}
	}
	if raw, ok := read(ctx, store, FailuresKey(arm)); ok {
		if f, err := ParseFailures(FailuresKey(arm), raw); err == nil {
			s.Failures = &f
		} else {
			slog.Warn("jevtrialsettings: ignoring the stored arm failure counter", "arm", arm, "error", err)
		}
	}
	if raw, ok := read(ctx, store, ThrottleKey(arm)); ok {
		if t, err := ParseThrottle(ThrottleKey(arm), raw); err == nil {
			s.Throttle = &t
		} else {
			slog.Warn("jevtrialsettings: ignoring the stored arm throttle record", "arm", arm, "error", err)
		}
	}
	return s
}

func read(ctx context.Context, store Store, key string) (string, bool) {
	raw, ok, err := store.Get(ctx, key)
	if err != nil {
		slog.Warn("jevtrialsettings: read runtime setting", "key", key, "error", err)
		return "", false
	}
	return raw, ok
}
