// Package jevshadowsettings defines the runtime_settings keys of the Jev
// prompt versioning and shadow comparison (issue #872): which Trader prompt
// version is live, whether a candidate version runs in shadow (Paper only,
// never used for trading), which candidate, the shadow's daily LLM cost cap
// and the record of a day the shadow was stopped by that cap.
//
// Every key lives in runtime_settings (docs/architecture/er.md
// §runtime_settings) and is read by the application (jev.Trader / jev.Shadow,
// the Settings screen) and, read-only, by the Paper results MCP, which share
// the database but not memory. None of them is on the MCP settings_set
// whitelist (mcpwritesettings): the shadow switch, the candidate and the cost
// cap are changed on the Settings screen only; the live version changes
// through the explicit promotion (GUI or MCP jev_prompt_promote), which also
// writes jev_prompt_promotions.
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
	// Nothing stored means ProposedDailyCostCapUSD.
	KeyDailyCostCapUSD = "jev.shadow.daily_cost_cap_usd"
	// KeyHalt records the day the shadow was stopped by the cost cap (a JSON
	// object, Halt). The shadow runs again from the next JST day.
	KeyHalt = "jev.shadow.halt"
)

// Keys returns every key of this package.
func Keys() []string {
	return []string{KeyLiveVersion, KeyEnabled, KeyCandidateVersion, KeyDailyCostCapUSD, KeyHalt}
}

// DefaultEnabled is the shadow switch while nothing is stored.
const DefaultEnabled = false

// ProposedDailyCostCapUSD is the shadow's daily cost cap while nothing is
// stored, in USD per JST day.
//
// 暫定の案・未承認: this value is a proposal and has NOT been approved. The
// basis (call frequency, per-call cost, every assumption) is in the pull
// request of issue #872. The number is decided by a person; until then this
// constant is the only place the proposal lives, and the Settings screen, the
// MCP and the docs say "暫定・未承認" next to it.
const ProposedDailyCostCapUSD = 0.10

// MaxDailyCostCapUSD bounds a stored cap so a typo cannot lift it to an absurd
// value.
const MaxDailyCostCapUSD = 100.0

// MaxVersionLen bounds a version name.
const MaxVersionLen = 40

// HaltReasonDailyCostCap is the Halt.Reason of a day stopped by the cap.
const HaltReasonDailyCostCap = "daily_cost_cap"

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

// Halt is the stored record of a day stopped by the cost cap.
type Halt struct {
	// Date is the JST day ("YYYY-MM-DD") the shadow is stopped for.
	Date string `json:"date"`
	// Reason is a fixed code (HaltReasonDailyCostCap).
	Reason string `json:"reason"`
	// SpentUSD is the day's shadow cost when the cap was reached.
	SpentUSD float64 `json:"spent_usd"`
	// CapUSD is the cap that was reached.
	CapUSD float64 `json:"cap_usd"`
	// At is when the shadow was stopped (RFC 3339, UTC).
	At string `json:"at"`
}

// EncodeHalt renders h as the runtime_settings value of KeyHalt.
func EncodeHalt(h Halt) (string, error) {
	b, err := json.Marshal(h)
	if err != nil {
		return "", fmt.Errorf("jevshadowsettings: encode %s: %w", KeyHalt, err)
	}
	return string(b), nil
}

// ParseHalt decodes the runtime_settings value of KeyHalt.
func ParseHalt(jsonValue string) (Halt, error) {
	var h Halt
	if err := json.Unmarshal([]byte(jsonValue), &h); err != nil {
		return Halt{}, fmt.Errorf("jevshadowsettings: decode runtime setting %s=%q: %w", KeyHalt, jsonValue, err)
	}
	if _, err := time.Parse(time.DateOnly, h.Date); err != nil || h.Reason == "" {
		return Halt{}, fmt.Errorf("jevshadowsettings: runtime setting %s=%q needs a date and a reason", KeyHalt, jsonValue)
	}
	return h, nil
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
	// ProposedDailyCostCapUSD (unapproved).
	CapUSD       float64
	CapIsDefault bool
	// Halt is the recorded cost-cap stop, nil for none.
	Halt *Halt
}

// Load reads every key. A stored row that no longer parses (the application
// stores only valid ones) or a failed read is logged and falls back to the safe
// side: the shadow off, no candidate, the default cap, no halt record, the
// build's live version. A fault can therefore never turn the shadow on or
// change the live prompt.
func Load(ctx context.Context, store Store) Settings {
	s := Settings{CapUSD: ProposedDailyCostCapUSD, CapIsDefault: true}
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
	return s
}

// LoadLiveVersion reads only KeyLiveVersion: the stored live version, or ""
// while nothing (valid) is stored. jev.Trader reads it on every call, so it
// does not load the other keys.
func LoadLiveVersion(ctx context.Context, store Store) string {
	raw, ok := read(ctx, store, KeyLiveVersion)
	if !ok {
		return ""
	}
	v, err := ParseVersion(KeyLiveVersion, raw)
	if err != nil {
		slog.Warn("jevshadowsettings: ignoring the stored live version", "error", err)
		return ""
	}
	return v
}

func read(ctx context.Context, store Store, key string) (string, bool) {
	raw, ok, err := store.Get(ctx, key)
	if err != nil {
		slog.Warn("jevshadowsettings: read runtime setting", "key", key, "error", err)
		return "", false
	}
	return raw, ok
}
