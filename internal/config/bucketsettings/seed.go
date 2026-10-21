package bucketsettings

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"time"
)

// SeededMarkerKey is the runtime_settings row recording that SeedDefaults has
// run on this database. It is an internal marker, not a Settings key (it is
// not in Keys).
const SeededMarkerKey = "system.bucket_budgets_seeded"

// SeedStore is the runtime_settings access SeedDefaults needs;
// *system.RuntimeSettingsRepository implements it.
type SeedStore interface {
	Get(ctx context.Context, key string) (value string, ok bool, err error)
	SetIfAbsent(ctx context.Context, key, value string, updatedAt time.Time) (inserted bool, err error)
}

// SeedDefaults writes the initial budgets once per database: the stock bucket
// gets the whole operating capital (capitalJPY, whole yen rounded down) and the
// bond_etf bucket 0, so the existing behaviour does not change. While
// SeededMarkerKey is absent: if none of Keys has a stored row it stores both
// budgets, otherwise it stores nothing; either way it then stores the marker,
// so later calls - even after the owner reset the rows - do nothing. It never
// overwrites a row: every write is an insert-if-absent
// (RuntimeSettingsRepository.SetIfAbsent), so a value the Settings screen or the
// MCP stored between the read and the write survives the seed (issue #837).
//
// capitalJPY is the operating capital in force when seeding, so it must be
// read after papersettings.SeedDefaults.
//
// Premise: this build is Paper-only (internal/bootstrap/risk.go), so the seed
// runs unconditionally. It MUST be gated on Paper before any Live build is
// added.
//
// A partial write failure is NOT retried: the marker is written last, so the
// next start finds some rows stored and only writes the marker.
func SeedDefaults(ctx context.Context, store SeedStore, capitalJPY float64, now time.Time) error {
	if _, done, err := store.Get(ctx, SeededMarkerKey); err != nil {
		return fmt.Errorf("bucketsettings: read %s: %w", SeededMarkerKey, err)
	} else if done {
		return nil
	}
	anyStored := false
	for _, key := range Keys() {
		_, ok, err := store.Get(ctx, key)
		if err != nil {
			return fmt.Errorf("bucketsettings: read %s: %w", key, err)
		}
		anyStored = anyStored || ok
	}
	if !anyStored {
		defaults := []struct {
			key   string
			value float64
		}{
			{KeyStockBudgetJPY, math.Floor(capitalJPY)},
			{KeyBondETFBudgetJPY, 0},
		}
		for i, d := range defaults {
			if _, err := store.SetIfAbsent(ctx, d.key, strconv.FormatFloat(d.value, 'f', -1, 64), now); err != nil {
				return fmt.Errorf("bucketsettings: seed %s (%d/%d stored): %w", d.key, i, len(defaults), err)
			}
		}
	}
	if _, err := store.SetIfAbsent(ctx, SeededMarkerKey, "true", now); err != nil {
		return fmt.Errorf("bucketsettings: write %s: %w", SeededMarkerKey, err)
	}
	return nil
}
