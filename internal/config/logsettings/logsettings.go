// Package logsettings reads the Settings-screen log level and local log
// retention (issue #772). Both live in runtime_settings
// (docs/architecture/er/tables-system.md) under config.KeyLogLevel and
// config.KeyLogRetentionDays; there is no environment-variable source.
package logsettings

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"slices"
	"strings"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/logging"
)

// DefaultLevel is the log level while none is stored. INFO keeps the metrics
// records (requirements/non-functional.md §5.1) in the full log.
const DefaultLevel = slog.LevelInfo

// DefaultRetentionDays is non-functional.md §5's local-retention window while
// none is stored.
const DefaultRetentionDays = logging.DefaultRetentionDays

// MinRetentionDays and MaxRetentionDays bound the retention. The upper bound
// keeps a typo from disabling archival for a decade.
const (
	MinRetentionDays = 1
	MaxRetentionDays = 3650
)

// DefaultLevelText is DefaultLevel as the Settings screen shows it.
const DefaultLevelText = "info"

// Levels are the selectable log levels, most verbose first.
var Levels = []string{"debug", "info", "warn", "error"}

// Keys returns the runtime_settings keys this package owns.
func Keys() []string { return []string{config.KeyLogLevel, config.KeyLogRetentionDays} }

// IsKey reports whether key is one of Keys.
func IsKey(key string) bool { return key == config.KeyLogLevel || key == config.KeyLogRetentionDays }

// NormalizeLevel trims and lower-cases raw and checks it is one of Levels.
func NormalizeLevel(raw string) (string, error) {
	name := strings.ToLower(strings.TrimSpace(raw))
	if !slices.Contains(Levels, name) {
		return "", fmt.Errorf("logsettings: %q is not a log level; use one of %s", raw, strings.Join(Levels, ", "))
	}
	return name, nil
}

// ParseLevel decodes jsonValue - a runtime_settings.value, a JSON string of
// "debug", "info", "warn" or "error" (case-insensitive).
func ParseLevel(jsonValue string) (slog.Level, error) {
	var raw string
	if err := json.Unmarshal([]byte(jsonValue), &raw); err != nil {
		return 0, fmt.Errorf("logsettings: decode runtime setting %s=%q as a string: %w", config.KeyLogLevel, jsonValue, err)
	}
	name, err := NormalizeLevel(raw)
	if err != nil {
		return 0, fmt.Errorf("runtime setting %s=%q: %w", config.KeyLogLevel, jsonValue, err)
	}
	var level slog.Level
	_ = level.UnmarshalText([]byte(name)) // Levels are all valid slog names
	return level, nil
}

// ParseRetentionDays decodes jsonValue - a runtime_settings.value, a JSON
// integer - and checks it lies within MinRetentionDays..MaxRetentionDays.
func ParseRetentionDays(jsonValue string) (int, error) {
	var n float64
	if err := json.Unmarshal([]byte(jsonValue), &n); err != nil {
		return 0, fmt.Errorf("logsettings: decode runtime setting %s=%q as a number: %w", config.KeyLogRetentionDays, jsonValue, err)
	}
	if math.IsNaN(n) || n != math.Trunc(n) {
		return 0, fmt.Errorf("logsettings: runtime setting %s=%q must be a whole number of days", config.KeyLogRetentionDays, jsonValue)
	}
	if n < MinRetentionDays || n > MaxRetentionDays {
		return 0, fmt.Errorf("logsettings: runtime setting %s=%q must be %d〜%d days", config.KeyLogRetentionDays, jsonValue, MinRetentionDays, MaxRetentionDays)
	}
	return int(n), nil
}

// Store is the runtime_settings read access the loaders need;
// *system.RuntimeSettingsRepository implements it.
type Store interface {
	Get(ctx context.Context, key string) (value string, ok bool, err error)
}

// LoadLevel returns the effective log level: the stored row, or DefaultLevel
// when there is none. A stored row that no longer parses or a failed store
// read is reported on the current default logger and DefaultLevel applies, so
// a fault can neither silence the log nor keep the application from logging.
func LoadLevel(ctx context.Context, store Store) slog.Level {
	raw, ok, err := store.Get(ctx, config.KeyLogLevel)
	if err == nil && !ok {
		return DefaultLevel
	}
	if err == nil {
		var level slog.Level
		if level, err = ParseLevel(raw); err == nil {
			return level
		}
	}
	slog.Warn("logsettings: using the default log level", "default", DefaultLevel.String(), "error", err)
	return DefaultLevel
}

// LoadRetentionDays returns the effective local log retention in days: the
// stored row, or DefaultRetentionDays when there is none. A stored row that
// no longer parses or a failed store read is logged and the default applies,
// so a fault can neither delete logs early nor stop archival.
func LoadRetentionDays(ctx context.Context, store Store) int {
	raw, ok, err := store.Get(ctx, config.KeyLogRetentionDays)
	if err == nil && !ok {
		return DefaultRetentionDays
	}
	if err == nil {
		var days int
		if days, err = ParseRetentionDays(raw); err == nil {
			return days
		}
	}
	slog.Warn("logsettings: using the default log retention", "default_days", DefaultRetentionDays, "error", err)
	return DefaultRetentionDays
}

// RetentionSource returns the retention lookup for logging.WithRetentionSource:
// LoadRetentionDays on every call, so a value saved in the Settings screen
// applies on the Archiver's next pass without a restart.
func RetentionSource(store Store) func(context.Context) int {
	return func(ctx context.Context) int { return LoadRetentionDays(ctx, store) }
}
