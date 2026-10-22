// Package sleepguardsettings holds the Settings-screen switch of the
// sleep guard (issue #863): while it is on, the app keeps the PC from going to
// sleep during a trading day's watch hours (08:45 JST up to the close), so a
// sleeping PC cannot stop the market-data collection. Outside those hours,
// with the switch off, and when the app exits, the OS power settings apply
// again.
//
// The switch lives in runtime_settings (docs/architecture/er.md
// §runtime_settings) under Key; there is no yaml or environment source. It is
// off by default and is deliberately not in the Paper results MCP whitelist
// (mcpwritesettings): a remote client must not change the PC's power
// behaviour.
package sleepguardsettings

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
)

// Key is the runtime_settings key: a JSON integer, 1 (on) or 0 (off).
const Key = "power.sleep_guard_enabled"

// Keys returns every runtime_settings key Apply accepts.
func Keys() []string { return []string{Key} }

// IsKey reports whether key is Key.
func IsKey(key string) bool { return key == Key }

// Default is the shipped value: off.
const Default = false

// Text renders enabled as the Settings screen shows it: "1" or "0".
func Text(enabled bool) string {
	if enabled {
		return "1"
	}
	return "0"
}

// Apply decodes jsonValue - a runtime_settings.value, the JSON integer 0 or 1.
func Apply(jsonValue string) (enabled bool, err error) {
	var n *float64 // a pointer, so JSON null is told from 0
	if err := json.Unmarshal([]byte(jsonValue), &n); err != nil {
		return false, fmt.Errorf("sleepguardsettings: decode runtime setting %s=%q as a number: %w", Key, jsonValue, err)
	}
	if n == nil || (*n != 0 && *n != 1) {
		return false, fmt.Errorf("sleepguardsettings: runtime setting %s=%q must be 0（無効）または 1（有効）", Key, jsonValue)
	}
	return *n == 1, nil
}

// Store is the runtime_settings read access Source needs;
// *system.RuntimeSettingsRepository implements it.
type Store interface {
	Get(ctx context.Context, key string) (value string, ok bool, err error)
}

// Source reads the switch at every check, so a change applies without a
// restart.
type Source struct{ Store Store }

// SleepGuardEnabled reports whether the switch is on. A stored row that no
// longer parses (the Settings screen stores only valid ones) is logged and
// counts as off; only a store read failure is an error.
func (s Source) SleepGuardEnabled(ctx context.Context) (bool, error) {
	raw, ok, err := s.Store.Get(ctx, Key)
	if err != nil {
		return false, fmt.Errorf("sleepguardsettings: read %s: %w", Key, err)
	}
	if !ok {
		return Default, nil
	}
	enabled, err := Apply(raw)
	if err != nil {
		slog.Warn("sleepguardsettings: ignoring invalid stored setting; the sleep guard stays off", "error", err)
		return Default, nil
	}
	return enabled, nil
}
