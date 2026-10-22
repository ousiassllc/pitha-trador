// Package paperpolicysettings holds the Paper-only entry-quality thresholds of
// the Policy Engine (issue #861): the lowest Jev entry_quality a candidate
// needs to enter, set per side for Paper Trading on its own.
//
// policy.long|short.min_entry_quality (internal/config, yaml default strong)
// stay the Live thresholds. In Paper the Policy Engine uses the keys of this
// package instead (policy.WithPaperEntryQuality; the trading mode is checked
// before the keys are read), because Jev almost never answers strong: over 30
// days only 4 of 1,290 answers did, and none entered. Paper therefore defaults
// to good; every other threshold is shared with Live.
//
// The keys live in runtime_settings (docs/architecture/er.md §runtime_settings)
// and are named after risk.paper.*: policy.paper.<side>.min_entry_quality.
// The values are JSON strings, validated like policy.*.min_entry_quality
// (config.IsEntryQuality). The Settings screen and the Paper results MCP
// (mcpwritesettings) both write them; no yaml or environment source exists.
package paperpolicysettings

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// runtime_settings keys; every value is a JSON string (an entry quality).
const (
	KeyLongMinEntryQuality  = "policy.paper.long.min_entry_quality"
	KeyShortMinEntryQuality = "policy.paper.short.min_entry_quality"
)

// Default is the value while nothing is stored: good.
const Default = domain.JevEntryQualityGood

// Keys returns every runtime_settings key of this package, in display order.
func Keys() []string { return []string{KeyLongMinEntryQuality, KeyShortMinEntryQuality} }

// IsKey reports whether key is one of Keys.
func IsKey(key string) bool { return key == KeyLongMinEntryQuality || key == KeyShortMinEntryQuality }

// PaperKeyOf returns the Paper key that stands in for liveKey, one of
// policy.long.min_entry_quality and policy.short.min_entry_quality.
func PaperKeyOf(liveKey string) string {
	return strings.Replace(liveKey, "policy.", "policy.paper.", 1)
}

// Settings is the Paper minimum entry quality per side.
type Settings struct {
	LongMinEntryQuality  string
	ShortMinEntryQuality string
}

// Defaults returns the settings while nothing is stored.
func Defaults() Settings {
	return Settings{LongMinEntryQuality: Default, ShortMinEntryQuality: Default}
}

// Apply decodes jsonValue - a runtime_settings.value, a JSON string - into the
// field of s named by key (one of Keys). The value must be one of the
// JevEntryQuality* strings.
func (s *Settings) Apply(key, jsonValue string) error {
	if !IsKey(key) {
		return fmt.Errorf("paperpolicysettings: %q is not a Paper policy setting key", key)
	}
	var v string
	if err := json.Unmarshal([]byte(jsonValue), &v); err != nil {
		return fmt.Errorf("paperpolicysettings: decode runtime setting %s=%q as a string: %w", key, jsonValue, err)
	}
	if !config.IsEntryQuality(v) {
		return fmt.Errorf("paperpolicysettings: %s must be one of poor, fair, good, strong, exceptional (got %q)", key, v)
	}
	if key == KeyLongMinEntryQuality {
		s.LongMinEntryQuality = v
	} else {
		s.ShortMinEntryQuality = v
	}
	return nil
}

// Store is the runtime_settings read access Source needs;
// *system.RuntimeSettingsRepository implements it.
type Store interface {
	Get(ctx context.Context, key string) (value string, ok bool, err error)
}

// Source reads the Paper thresholds at every decision, so a change applies to
// the next decision without a restart. It implements
// policy.PaperEntryQualitySource.
type Source struct{ Store Store }

// PaperEntryQuality returns the stored Paper thresholds, Default for a key
// with no row. A stored row that no longer parses (the Settings screen and the
// MCP store only valid ones) is logged and counts as Default; only a store
// read failure is an error.
func (s Source) PaperEntryQuality(ctx context.Context) (Settings, error) {
	out := Defaults()
	for _, key := range Keys() {
		raw, ok, err := s.Store.Get(ctx, key)
		if err != nil {
			return Settings{}, fmt.Errorf("paperpolicysettings: read %s: %w", key, err)
		}
		if !ok {
			continue
		}
		if err := out.Apply(key, raw); err != nil {
			slog.Warn("paperpolicysettings: ignoring invalid stored setting; the default applies", "key", key, "error", err)
		}
	}
	return out, nil
}
