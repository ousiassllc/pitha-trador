// Package jevshadowsettings defines the runtime_settings keys of the Jev
// prompt versioning and shadow comparison (issue #872): which Trader prompt
// version is live, whether a candidate version runs in shadow (Paper only,
// never used for trading), which candidate, the shadow's daily LLM cost cap
// and the record of a day the shadow was stopped by that cap.
//
// Every key lives in runtime_settings (docs/architecture/er.md
// §runtime_settings) and is read by the application (jev.Trader / jev/shadow.Shadow,
// the Settings screen) and by the Paper results MCP, which share the database
// but not memory. The MCP reads all of them; the only ones on its settings_set
// whitelist (mcpwritesettings, issue #929) are the shadow switch (KeyEnabled)
// and the candidate (KeyCandidateVersion). The cost cap, the failure and
// throttle records and the live version are not: the cap is changed on the
// Settings screen only, and the live version changes through the explicit
// promotion (GUI or MCP jev_prompt_promote), which also writes
// jev_prompt_promotions.
//
// Nothing here is read by Policy, the meta-model, Risk or order placement: the
// shadow never influences trading.
package jevshadowsettings

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"time"
	"unicode"
)

// Keys of the runtime_settings rows.
const (
	// KeyLiveVersion is the live Trader prompt version (a JSON string such as
	// "trader-v3"). Nothing stored means the build's default
	// (jev.TraderQuestionVersion). Only a promotion writes it.
	KeyLiveVersion = "jev.trader_prompt_version"
	// KeyEnabled is the shadow switch: the JSON integer 1 (on) or 0 (off).
	// OFF while nothing is stored or the stored value is damaged: the shadow
	// costs money, so a person turns it on.
	KeyEnabled = "jev.shadow.enabled"
	// KeyCandidateVersion is the candidate Trader prompt version the shadow
	// runs (a JSON string). Nothing stored means no candidate: an enabled
	// shadow with no candidate does nothing.
	KeyCandidateVersion = "jev.shadow.candidate_version"
	// KeyDailyCostCapUSD is the shadow's daily cost cap in USD (a JSON number).
	// Nothing stored means DefaultDailyCostCapUSD.
	KeyDailyCostCapUSD = "jev.shadow.daily_cost_cap_usd"
	// KeyHalt records the day the shadow was stopped by the cost or failure cap
	// (a JSON object, Halt). The shadow runs again from the next JST day.
	KeyHalt = "jev.shadow.halt"
	// KeyFailures counts the day's failed shadow calls (a JSON object,
	// Failures; halt.go).
	KeyFailures = "jev.shadow.failures"
	// KeyThrottle records the last time the Jev API answered with 429 or 529
	// and the shadow stopped sending because of it (a JSON object, Throttle;
	// throttle.go).
	KeyThrottle = "jev.shadow.throttle"
)

// Keys returns every key of this package.
func Keys() []string {
	return []string{KeyLiveVersion, KeyEnabled, KeyCandidateVersion, KeyDailyCostCapUSD, KeyHalt, KeyFailures, KeyThrottle}
}

// DefaultEnabled is the shadow switch while nothing is stored.
const DefaultEnabled = false

// DefaultDailyCostCapUSD is the shadow's daily cost cap while nothing is
// stored, in USD per JST day.
//
// 0.10 USD a day is a user decision (確定). It is the default: the Settings
// screen can raise or lower it (a stored value wins; an empty save removes the
// stored value and this one applies again). The basis (call frequency,
// per-call cost, every assumption) is in the pull request of issue #872.
const DefaultDailyCostCapUSD = 0.10

// MaxDailyCostCapUSD bounds a stored cap so a typo cannot lift it to an absurd
// value.
const MaxDailyCostCapUSD = 100.0

// MaxVersionLen bounds a version name.
const MaxVersionLen = 40

// JST is the fixed Asia/Tokyo offset: the shadow's "day" is a JST day, like the
// trading session.
var JST = time.FixedZone("JST", 9*60*60)

// Day returns t's JST calendar day as "YYYY-MM-DD".
func Day(t time.Time) string { return t.In(JST).Format(time.DateOnly) }

// DayBounds returns the half-open UTC interval [from, to) of the JST day that
// contains t.
func DayBounds(t time.Time) (from, to time.Time) {
	local := t.In(JST)
	start := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, JST)
	return start.UTC(), start.AddDate(0, 0, 1).UTC()
}

// Switch renders a switch state as stored: "1" or "0".
func Switch(enabled bool) string {
	if enabled {
		return "1"
	}
	return "0"
}

// ParseEnabled decodes the runtime_settings value of KeyEnabled (the JSON
// integer 0 or 1).
func ParseEnabled(jsonValue string) (bool, error) {
	var n *float64
	if err := json.Unmarshal([]byte(jsonValue), &n); err != nil {
		return false, fmt.Errorf("jevshadowsettings: decode runtime setting %s=%q as a number: %w", KeyEnabled, jsonValue, err)
	}
	if n == nil || (*n != 0 && *n != 1) {
		return false, fmt.Errorf("jevshadowsettings: runtime setting %s=%q must be 0 or 1", KeyEnabled, jsonValue)
	}
	return *n == 1, nil
}

// ValidateVersion checks a version name's shape (not that it is registered:
// the registry lives in internal/service/jev).
func ValidateVersion(v string) error {
	if v == "" || len(v) > MaxVersionLen {
		return fmt.Errorf("jevshadowsettings: a prompt version must be 1〜%d characters", MaxVersionLen)
	}
	for _, r := range v {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			return fmt.Errorf("jevshadowsettings: a prompt version must not contain spaces or control characters")
		}
	}
	return nil
}

// EncodeVersion renders v as a runtime_settings JSON string.
func EncodeVersion(v string) (string, error) {
	if err := ValidateVersion(v); err != nil {
		return "", err
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("jevshadowsettings: encode version: %w", err)
	}
	return string(b), nil
}

// ParseVersion decodes a runtime_settings JSON string holding a version name.
func ParseVersion(key, jsonValue string) (string, error) {
	var v string
	if err := json.Unmarshal([]byte(jsonValue), &v); err != nil {
		return "", fmt.Errorf("jevshadowsettings: decode runtime setting %s=%q as a string: %w", key, jsonValue, err)
	}
	if err := ValidateVersion(v); err != nil {
		return "", fmt.Errorf("jevshadowsettings: runtime setting %s=%q: %w", key, jsonValue, err)
	}
	return v, nil
}

// ValidateCap checks a daily cost cap: finite, above 0 and at most
// MaxDailyCostCapUSD. 0 is refused so the cap can never be switched off by
// accident; the way to stop the shadow is its switch.
func ValidateCap(usd float64) error {
	if math.IsNaN(usd) || math.IsInf(usd, 0) || usd <= 0 || usd > MaxDailyCostCapUSD {
		return fmt.Errorf("jevshadowsettings: the daily cost cap must be above 0 and at most %g USD", MaxDailyCostCapUSD)
	}
	return nil
}

// ParseCap decodes the runtime_settings value of KeyDailyCostCapUSD.
func ParseCap(jsonValue string) (float64, error) {
	var n float64
	if err := json.Unmarshal([]byte(jsonValue), &n); err != nil {
		return 0, fmt.Errorf("jevshadowsettings: decode runtime setting %s=%q as a number: %w", KeyDailyCostCapUSD, jsonValue, err)
	}
	if err := ValidateCap(n); err != nil {
		return 0, fmt.Errorf("jevshadowsettings: runtime setting %s=%q: %w", KeyDailyCostCapUSD, jsonValue, err)
	}
	return n, nil
}

// Store is the runtime_settings read access Load needs;
// *system.RuntimeSettingsRepository implements it.
type Store interface {
	Get(ctx context.Context, key string) (value string, ok bool, err error)
}

// Settings is the effective state of every key.
type Settings struct {
	// LiveVersion is the stored live version, "" while nothing is stored (the
	// build default applies; internal/service/jev resolves it).
	LiveVersion string
	// Enabled is the shadow switch.
	Enabled bool
	// CandidateVersion is the shadow's candidate, "" for none.
	CandidateVersion string
	// CapUSD is the daily cost cap in USD; CapIsDefault is true while it is
	// DefaultDailyCostCapUSD (the default, not a stored value).
	CapUSD       float64
	CapIsDefault bool
	// Halt is the recorded cost- or failure-cap stop, nil for none.
	Halt *Halt
	// Failures is the stored failed-call counter, nil for none.
	Failures *Failures
	// Throttle is the recorded 429/529 stop, nil for none.
	Throttle *Throttle
}

// Load reads every key. A stored row that no longer parses (the application
// stores only valid ones) or a failed read is logged and falls back to the safe
// side: the shadow off, no candidate, the default cap, no halt record, the
// build's live version. A fault can therefore never turn the shadow on or
// change the live prompt.
func Load(ctx context.Context, store Store) Settings {
	s := Settings{CapUSD: DefaultDailyCostCapUSD, CapIsDefault: true}
	if raw, ok := read(ctx, store, KeyLiveVersion); ok {
		if v, err := ParseVersion(KeyLiveVersion, raw); err == nil {
			s.LiveVersion = v
		} else {
			slog.Warn("jevshadowsettings: ignoring the stored live version", "error", err)
		}
	}
	if raw, ok := read(ctx, store, KeyEnabled); ok {
		if on, err := ParseEnabled(raw); err == nil {
			s.Enabled = on
		} else {
			slog.Warn("jevshadowsettings: treating a damaged shadow switch as off", "error", err)
		}
	}
	if raw, ok := read(ctx, store, KeyCandidateVersion); ok {
		if v, err := ParseVersion(KeyCandidateVersion, raw); err == nil {
			s.CandidateVersion = v
		} else {
			slog.Warn("jevshadowsettings: ignoring the stored candidate version", "error", err)
		}
	}
	if raw, ok := read(ctx, store, KeyDailyCostCapUSD); ok {
		if c, err := ParseCap(raw); err == nil {
			s.CapUSD, s.CapIsDefault = c, false
		} else {
			slog.Warn("jevshadowsettings: using the default daily cost cap", "error", err)
		}
	}
	if raw, ok := read(ctx, store, KeyHalt); ok {
		if h, err := ParseHalt(raw); err == nil {
			s.Halt = &h
		} else {
			slog.Warn("jevshadowsettings: ignoring the stored halt record", "error", err)
		}
	}
	if raw, ok := read(ctx, store, KeyFailures); ok {
		if f, err := ParseFailures(raw); err == nil {
			s.Failures = &f
		} else {
			slog.Warn("jevshadowsettings: ignoring the stored failure counter", "error", err)
		}
	}
	if raw, ok := read(ctx, store, KeyThrottle); ok {
		if t, err := ParseThrottle(raw); err == nil {
			s.Throttle = &t
		} else {
			slog.Warn("jevshadowsettings: ignoring the stored throttle record", "error", err)
		}
	}
	return s
}

// LoadLiveVersion reads only KeyLiveVersion: the stored live version, or ""
// while nothing (valid) is stored. jev.Trader reads it on every call, so it
// does not load the other keys. A failed READ is an error (the caller must not
// mistake "could not read" for "nothing promoted"); a stored value that does
// not parse is logged and treated as nothing stored.
func LoadLiveVersion(ctx context.Context, store Store) (string, error) {
	raw, ok, err := store.Get(ctx, KeyLiveVersion)
	if err != nil {
		return "", fmt.Errorf("jevshadowsettings: read the live prompt version: %w", err)
	}
	if !ok {
		return "", nil
	}
	v, err := ParseVersion(KeyLiveVersion, raw)
	if err != nil {
		slog.Warn("jevshadowsettings: ignoring the stored live version", "error", err)
		return "", nil
	}
	return v, nil
}

func read(ctx context.Context, store Store, key string) (string, bool) {
	raw, ok, err := store.Get(ctx, key)
	if err != nil {
		slog.Warn("jevshadowsettings: read runtime setting", "key", key, "error", err)
		return "", false
	}
	return raw, ok
}
