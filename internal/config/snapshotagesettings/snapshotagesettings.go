// Package snapshotagesettings holds the Settings-screen limit on how old a
// market_snapshots bar may be before Jev Scout/Trader and Paper Entry refuse
// to act on it in ranking-watch mode (issue #754). It lives in
// runtime_settings (docs/architecture/er.md §runtime_settings) under Key;
// there is no yaml or environment source. Full-scan mode keeps its own
// scan.full_scan_max_snapshot_age_seconds (config.ScanConfig.SnapshotMaxAge).
package snapshotagesettings

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// Key is the runtime_settings key; its value is a JSON integer (seconds).
const Key = "scan.snapshot_max_age_seconds"

// MinSeconds and MaxSeconds bound the limit. The upper bound keeps a typo
// from silently re-admitting hour-old bars.
const (
	MinSeconds = 1
	MaxSeconds = 600
)

// DefaultSeconds is what applies while nothing is stored.
const DefaultSeconds = int(domain.DefaultMaxSnapshotAge / time.Second)

// IsKey reports whether key is Key.
func IsKey(key string) bool { return key == Key }

// Parse decodes jsonValue - a runtime_settings.value, a JSON integer of
// seconds - and checks it lies within MinSeconds..MaxSeconds.
func Parse(jsonValue string) (time.Duration, error) {
	var n float64
	if err := json.Unmarshal([]byte(jsonValue), &n); err != nil {
		return 0, fmt.Errorf("snapshotagesettings: decode runtime setting %s=%q as a number: %w", Key, jsonValue, err)
	}
	if math.IsNaN(n) || n != math.Trunc(n) {
		return 0, fmt.Errorf("snapshotagesettings: runtime setting %s=%q must be a whole number of seconds", Key, jsonValue)
	}
	if n < MinSeconds || n > MaxSeconds {
		return 0, fmt.Errorf("snapshotagesettings: runtime setting %s=%q must be %d〜%d seconds", Key, jsonValue, MinSeconds, MaxSeconds)
	}
	return time.Duration(n) * time.Second, nil
}

// Store is the runtime_settings read access Load needs;
// *system.RuntimeSettingsRepository implements it.
type Store interface {
	Get(ctx context.Context, key string) (value string, ok bool, err error)
}

// Load returns the effective ranking-watch snapshot max age: the stored row,
// or domain.DefaultMaxSnapshotAge when there is none. A stored row that no
// longer parses (the Settings screen stores only valid ones) or a failed
// store read is logged and the default applies, so a fault can neither widen
// nor disable the gate.
func Load(ctx context.Context, store Store) time.Duration {
	raw, ok, err := store.Get(ctx, Key)
	if err == nil && !ok {
		return domain.DefaultMaxSnapshotAge
	}
	if err == nil {
		var age time.Duration
		if age, err = Parse(raw); err == nil {
			return age
		}
	}
	slog.Warn("snapshotagesettings: using the default snapshot max age", "default", domain.DefaultMaxSnapshotAge, "error", err)
	return domain.DefaultMaxSnapshotAge
}
