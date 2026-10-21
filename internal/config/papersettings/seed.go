package papersettings

import (
	"context"
	"fmt"
	"strconv"
	"time"
)

// SeededMarkerKey is the runtime_settings row recording that SeedDefaults has
// run on this database. It is an internal marker, not a Settings key (it is
// not in Keys).
const SeededMarkerKey = "system.paper_risk_defaults_seeded"

// SeedStore is the runtime_settings access SeedDefaults needs;
// *system.RuntimeSettingsRepository implements it.
type SeedStore interface {
	Get(ctx context.Context, key string) (value string, ok bool, err error)
	SetIfAbsent(ctx context.Context, key, value string, updatedAt time.Time) (inserted bool, err error)
}

// SeedDefaults writes the Paper initial values (DefaultInitialCapitalJPY,
// DefaultMaxPositionPerSymbolPct, DefaultMaxTotalExposurePct,
// DefaultMaxTradeLossPct) once per database, so an existing install whose
// on-disk config/risk.yaml still has the old Paper values (30,000,000 円 / 2% /
// 20% / 0.25%) starts from the issue #804 decision too. While SeededMarkerKey
// is absent: if none of Keys has a stored row it stores the four values,
// otherwise it stores nothing; either way it then stores the marker, so later
// calls - even after the owner reset the rows to the yaml values - do nothing.
// It never overwrites a row: every write is an insert-if-absent
// (RuntimeSettingsRepository.SetIfAbsent), so a value the Settings screen or the
// MCP stored between the read and the write survives the seed (issue #837).
//
// Premise: this build is Paper-only (internal/bootstrap/risk.go), so the seed
// runs unconditionally. It MUST be gated on Paper before any Live build is
// added.
//
// A partial write failure is NOT retried: the marker is written last, so the
// next start finds some rows stored and only writes the marker.
func SeedDefaults(ctx context.Context, store SeedStore, now time.Time) error {
	if _, done, err := store.Get(ctx, SeededMarkerKey); err != nil {
		return fmt.Errorf("papersettings: read %s: %w", SeededMarkerKey, err)
	} else if done {
		return nil
	}
	anyStored := false
	for _, key := range Keys() {
		_, ok, err := store.Get(ctx, key)
		if err != nil {
			return fmt.Errorf("papersettings: read %s: %w", key, err)
		}
		anyStored = anyStored || ok
	}
	if !anyStored {
		defaults := []struct {
			key   string
			value float64
		}{
			{KeyInitialCapitalJPY, DefaultInitialCapitalJPY},
			{KeyMaxPositionPerSymbolPct, DefaultMaxPositionPerSymbolPct},
			{KeyMaxTotalExposurePct, DefaultMaxTotalExposurePct},
			{KeyMaxTradeLossPct, DefaultMaxTradeLossPct},
		}
		for i, d := range defaults {
			if _, err := store.SetIfAbsent(ctx, d.key, strconv.FormatFloat(d.value, 'f', -1, 64), now); err != nil {
				return fmt.Errorf("papersettings: seed %s (%d/%d stored): %w", d.key, i, len(defaults), err)
			}
		}
	}
	if _, err := store.SetIfAbsent(ctx, SeededMarkerKey, "true", now); err != nil {
		return fmt.Errorf("papersettings: write %s: %w", SeededMarkerKey, err)
	}
	return nil
}
