// Package paperbootstrapsettings holds the Settings-screen switch of the
// Paper-only bootstrap entry (issue #798): while no meta-labeling model is
// adopted, every candidate is skipped before the Risk Engine, so Paper opens
// no positions and the labels the first model needs never accumulate. With
// the switch on - and only in Paper - the Policy Engine enters on the legacy
// threshold rule instead (Jev's confidence against policy.*.min_probability,
// with the confidence-bucket calibration gate), until a model is adopted.
//
// The switch lives in runtime_settings (docs/architecture/er.md
// §runtime_settings) under Key; there is no yaml or environment source. It is
// off by default and has no effect outside Paper: the Policy Engine checks the
// trading mode before it reads the switch (policy/paperbootstrap.Entry.Active,
// wired by policy.WithPaperBootstrap), and the Settings service refuses to
// store it for any other mode.
package paperbootstrapsettings

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
)

// Key is the runtime_settings key: a JSON integer, 1 (on) or 0 (off).
const Key = "paper.bootstrap_entry_enabled"

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
	var n float64
	if err := json.Unmarshal([]byte(jsonValue), &n); err != nil {
		return false, fmt.Errorf("paperbootstrapsettings: decode runtime setting %s=%q as a number: %w", Key, jsonValue, err)
	}
	if n != 0 && n != 1 {
		return false, fmt.Errorf("paperbootstrapsettings: runtime setting %s=%q must be 0（無効）または 1（有効）", Key, jsonValue)
	}
	return n == 1, nil
}

// Store is the runtime_settings read access Load needs;
// *system.RuntimeSettingsRepository implements it.
type Store interface {
	Get(ctx context.Context, key string) (value string, ok bool, err error)
}

// Source reads the switch at every decision, so a change applies without a
// restart. It implements policy/paperbootstrap.Source.
type Source struct{ Store Store }

// PaperBootstrapEnabled reports whether the switch is on. A stored row that no
// longer parses (the Settings screen stores only valid ones) is logged and
// counts as off; only a store read failure is an error.
func (s Source) PaperBootstrapEnabled(ctx context.Context) (bool, error) {
	raw, ok, err := s.Store.Get(ctx, Key)
	if err != nil {
		return false, fmt.Errorf("paperbootstrapsettings: read %s: %w", Key, err)
	}
	if !ok {
		return Default, nil
	}
	enabled, err := Apply(raw)
	if err != nil {
		slog.Warn("paperbootstrapsettings: ignoring invalid stored setting; the bootstrap entry stays off", "error", err)
		return Default, nil
	}
	return enabled, nil
}
