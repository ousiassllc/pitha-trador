// Package researchlabelsettings holds the Settings-screen switch and the
// retention of the research-only horizon labels (issues #1074, #1073): whether
// the daily maintenance task `research_labels` records them at all, and how many
// JST days of the research label rows it keeps.
//
// Both keys live in runtime_settings (docs/architecture/er.md §runtime_settings);
// there is no yaml or environment source. The recording writes only research
// tables, never a trading input, but it is still a write the trading process
// makes, so it is OFF by default: while nothing is stored (or the stored value is
// damaged) the task writes nothing. The switch (KeyEnabled) is on the Paper
// results MCP's settings_set whitelist (mcpwritesettings) because turning it on
// or off moves no money; the retention is changed on the Settings screen only.
package researchlabelsettings

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
)

// Keys of the runtime_settings rows.
const (
	// KeyEnabled is the switch: the JSON integer 1 (record) or 0 (do not).
	KeyEnabled = "research.labels.enabled"
	// KeyRetentionDays is how many JST days of the research label rows
	// the task keeps: a JSON integer, MinRetentionDays..MaxRetentionDays.
	KeyRetentionDays = "research.labels.retention_days"
)

// DefaultEnabled is the switch while nothing is stored: off.
const DefaultEnabled = false

// Retention bounds and default, in JST days. 90 days is the retention of
// market_snapshots, the source the rows are made from.
const (
	MinRetentionDays     = 1
	MaxRetentionDays     = 3650
	DefaultRetentionDays = 90
)

// Keys returns every runtime_settings key of this package.
func Keys() []string { return []string{KeyEnabled, KeyRetentionDays} }

// IsKey reports whether key is one of Keys.
func IsKey(key string) bool { return key == KeyEnabled || key == KeyRetentionDays }

// Text renders a switch state as stored: "1" or "0".
func Text(enabled bool) string {
	if enabled {
		return "1"
	}
	return "0"
}

// ParseEnabled decodes the runtime_settings value of KeyEnabled (the JSON
// integer 0 or 1).
func ParseEnabled(jsonValue string) (bool, error) {
	var n *float64 // a pointer, so JSON null is told from 0
	if err := json.Unmarshal([]byte(jsonValue), &n); err != nil {
		return false, fmt.Errorf("researchlabelsettings: decode runtime setting %s=%q as a number: %w", KeyEnabled, jsonValue, err)
	}
	if n == nil || (*n != 0 && *n != 1) {
		return false, fmt.Errorf("researchlabelsettings: runtime setting %s=%q must be 0（無効）または 1（有効）", KeyEnabled, jsonValue)
	}
	return *n == 1, nil
}

// ParseRetentionDays decodes the runtime_settings value of KeyRetentionDays and
// checks it lies within MinRetentionDays..MaxRetentionDays.
func ParseRetentionDays(jsonValue string) (int, error) {
	var n float64
	if err := json.Unmarshal([]byte(jsonValue), &n); err != nil {
		return 0, fmt.Errorf("researchlabelsettings: decode runtime setting %s=%q as a number: %w", KeyRetentionDays, jsonValue, err)
	}
	if math.IsNaN(n) || n != math.Trunc(n) {
		return 0, fmt.Errorf("researchlabelsettings: runtime setting %s=%q must be a whole number of days", KeyRetentionDays, jsonValue)
	}
	if n < MinRetentionDays || n > MaxRetentionDays {
		return 0, fmt.Errorf("researchlabelsettings: runtime setting %s=%q must be %d〜%d days", KeyRetentionDays, jsonValue, MinRetentionDays, MaxRetentionDays)
	}
	return int(n), nil
}

// Store is the runtime_settings read access Load needs;
// *system.RuntimeSettingsRepository implements it.
type Store interface {
	Get(ctx context.Context, key string) (value string, ok bool, err error)
}

// Settings is the effective state of both keys.
type Settings struct {
	Enabled       bool
	RetentionDays int
}

// Load reads the effective settings. A key with nothing stored, or a stored row
// that does not parse (the Settings screen and the MCP store only valid ones), is
// logged if damaged and counts as its default, so a fault can neither switch the
// recording on nor shorten the retention. Only a failed store read is an error:
// the caller must not guess then.
func Load(ctx context.Context, store Store) (Settings, error) {
	s := Settings{Enabled: DefaultEnabled, RetentionDays: DefaultRetentionDays}
	raw, ok, err := store.Get(ctx, KeyEnabled)
	if err != nil {
		return Settings{}, fmt.Errorf("researchlabelsettings: read %s: %w", KeyEnabled, err)
	}
	if ok {
		if on, perr := ParseEnabled(raw); perr != nil {
			slog.Warn("researchlabelsettings: ignoring invalid stored setting; the research labels stay off", "error", perr)
		} else {
			s.Enabled = on
		}
	}
	raw, ok, err = store.Get(ctx, KeyRetentionDays)
	if err != nil {
		return Settings{}, fmt.Errorf("researchlabelsettings: read %s: %w", KeyRetentionDays, err)
	}
	if ok {
		if days, perr := ParseRetentionDays(raw); perr != nil {
			slog.Warn("researchlabelsettings: ignoring invalid stored setting; the default retention applies", "default_days", DefaultRetentionDays, "error", perr)
		} else {
			s.RetentionDays = days
		}
	}
	return s, nil
}
