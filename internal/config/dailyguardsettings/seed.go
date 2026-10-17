package dailyguardsettings

import (
	"context"
	"fmt"
	"time"
)

// SeededMarkerKey is the runtime_settings row recording that
// SeedPaperDefaults has run on this database. It is an internal marker, not
// a Settings key (it is not in Keys).
const SeededMarkerKey = "system.daily_guard_defaults_seeded"

// paperDefaults are the initial stored values SeedPaperDefaults writes, as
// the JSON numbers the Settings screen stores. They are a rough starting
// guide for Paper Trading only (運用資金 約50万円・損失1%・発動0.5%・戻し50%・
// 最大20回); the owner must review the actual operating capital and tolerable
// loss before Live. They are seeded rather than coded as defaults so the
// risk engine's behaviour with nothing stored (everything off) is unchanged.
var paperDefaults = []struct{ key, value string }{
	{KeyOperatingCapitalJPY, "500000"},
	{KeyLossLimitPct, "1"},
	{KeyProfitLockTriggerPct, "0.5"},
	{KeyProfitLockGivebackPct, "50"},
	{KeyMaxTrades, "20"},
}

// SeedStore is the runtime_settings access SeedPaperDefaults needs;
// *system.RuntimeSettingsRepository implements it.
type SeedStore interface {
	Get(ctx context.Context, key string) (value string, ok bool, err error)
	Set(ctx context.Context, key, value string, updatedAt time.Time) error
}

// SeedPaperDefaults writes the Paper initial values once per database.
// While SeededMarkerKey is absent: if none of Keys has a stored row it stores
// the five paperDefaults, otherwise (e.g. a legacy yen setting exists) it
// stores nothing; either way it then stores the marker, so later calls - even
// after the owner deleted the rows - do nothing. "None stored" is about the
// rows, not the database's age: an upgraded existing Paper database with no
// daily guard rows is seeded too (once). It never overwrites a row.
//
// A partial write failure is NOT retried: the marker is written last, so if
// seeding stops midway the error is returned, the next start finds some
// rows stored and only writes the marker, and the rows already written stay
// as they are.
func SeedPaperDefaults(ctx context.Context, store SeedStore, now time.Time) error {
	if _, done, err := store.Get(ctx, SeededMarkerKey); err != nil {
		return fmt.Errorf("dailyguardsettings: read %s: %w", SeededMarkerKey, err)
	} else if done {
		return nil
	}
	anyStored := false
	for _, key := range Keys() {
		_, ok, err := store.Get(ctx, key)
		if err != nil {
			return fmt.Errorf("dailyguardsettings: read %s: %w", key, err)
		}
		anyStored = anyStored || ok
	}
	if !anyStored {
		for _, d := range paperDefaults {
			if err := store.Set(ctx, d.key, d.value, now); err != nil {
				return fmt.Errorf("dailyguardsettings: seed %s: %w", d.key, err)
			}
		}
	}
	if err := store.Set(ctx, SeededMarkerKey, "true", now); err != nil {
		return fmt.Errorf("dailyguardsettings: write %s: %w", SeededMarkerKey, err)
	}
	return nil
}
