// Package jevreplaysettings defines the runtime_settings key of the replay
// evaluation (issue #1085): whether the replay runner may send saved Jev inputs
// to the trial's arms. The replay reads the trial's arms, per-arm keys and
// daily cost cap (jevtrialsettings) and has no settings of its own besides this
// switch.
//
// The replay costs money and is record-only. It is OFF by default: a missing
// row or a damaged value means off, and with the switch off (or no key, or no
// arm) the runner makes no external request. Like jev.trial.enabled the switch
// is not on the MCP's settings_set whitelist (mcpwritesettings), is not a
// generic Settings key and cannot be changed by the self-improvement loop: a
// person turns it on on the Settings screen only. The MCP tools can start and
// stop a run but never touch this key.
package jevreplaysettings

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
)

// KeyEnabled is the replay switch: the JSON integer 1 (on) or 0 (off). OFF
// while nothing is stored or the stored value is damaged.
const KeyEnabled = "jev.replay.enabled"

// KeyPrefix starts every key of this package.
const KeyPrefix = "jev.replay."

// DefaultEnabled is the switch while nothing is stored.
const DefaultEnabled = false

// Keys returns the keys of this package.
func Keys() []string { return []string{KeyEnabled} }

// IsKey reports whether key is one of this package's keys.
func IsKey(key string) bool { return strings.HasPrefix(key, KeyPrefix) }

// ParseEnabled decodes the runtime_settings value of KeyEnabled (the JSON
// integer 0 or 1).
func ParseEnabled(jsonValue string) (bool, error) {
	var n *float64
	if err := json.Unmarshal([]byte(jsonValue), &n); err != nil {
		return false, fmt.Errorf("jevreplaysettings: decode runtime setting %s=%q as a number: %w", KeyEnabled, jsonValue, err)
	}
	if n == nil || (*n != 0 && *n != 1) {
		return false, fmt.Errorf("jevreplaysettings: runtime setting %s=%q must be 0 or 1", KeyEnabled, jsonValue)
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

// Store is the runtime_settings read access Enabled needs;
// *system.RuntimeSettingsRepository implements it.
type Store interface {
	Get(ctx context.Context, key string) (value string, ok bool, err error)
}

// Enabled reads the switch. A missing row is off; a damaged value is logged and
// is off; a failed read is returned as the error and is off too (the caller
// must not run on an unknown switch), so a fault can never turn the replay on.
func Enabled(ctx context.Context, store Store) (bool, error) {
	raw, ok, err := store.Get(ctx, KeyEnabled)
	if err != nil {
		return false, fmt.Errorf("jevreplaysettings: read runtime setting %s: %w", KeyEnabled, err)
	}
	if !ok {
		return DefaultEnabled, nil
	}
	on, err := ParseEnabled(raw)
	if err != nil {
		slog.Warn("jevreplaysettings: treating a damaged replay switch as off", "error", err)
		return false, nil
	}
	return on, nil
}
