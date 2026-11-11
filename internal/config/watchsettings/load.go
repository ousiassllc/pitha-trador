package watchsettings

import (
	"context"
	"log/slog"
)

// Store is the runtime_settings read access Load needs;
// *system.RuntimeSettingsRepository implements it.
type Store interface {
	Get(ctx context.Context, key string) (value string, ok bool, err error)
}

// Settings is the effective state of the operator-editable keys.
type Settings struct {
	// MaxWatched is the ranking watch list size (never above MaxKabuWatched).
	MaxWatched int
	// ObservationEnabled is the observation switch.
	ObservationEnabled bool
	// ObservationSymbols is the observation set as configured, whether or not
	// the switch is on; use Observed for the effective set.
	ObservationSymbols []string
	// ObservationCapUSD is the observation daily cost cap.
	ObservationCapUSD float64
}

// Default returns today's behaviour: the full kabu watch list, no observation
// symbol, the default cap.
func Default() Settings {
	return Settings{MaxWatched: DefaultMaxWatched, ObservationEnabled: DefaultObservationEnabled, ObservationCapUSD: DefaultObservationDailyCostCapUSD}
}

// Observed returns the effective observation-only symbols: the configured
// ones while the switch is on, none otherwise.
func (s Settings) Observed() []string {
	if !s.ObservationEnabled {
		return nil
	}
	return s.ObservationSymbols
}

// Load reads every key. A stored row that no longer parses (the application
// stores only valid ones) or a failed read is logged and falls back to the
// safe side: the default watch size, the observation off, the default cap. A
// fault can therefore never raise the watch size or turn the observation on.
func Load(ctx context.Context, store Store) Settings {
	s := Default()
	if raw, ok := read(ctx, store, KeyMaxWatched); ok {
		if n, err := ParseMaxWatched(raw); err == nil {
			s.MaxWatched = n
		} else {
			slog.Warn("watchsettings: using the default watch list size", "error", err)
		}
	}
	if raw, ok := read(ctx, store, KeyObservationEnabled); ok {
		if on, err := ParseEnabled(raw); err == nil {
			s.ObservationEnabled = on
		} else {
			slog.Warn("watchsettings: treating a damaged observation switch as off", "error", err)
		}
	}
	if raw, ok := read(ctx, store, KeyObservationSymbols); ok {
		if syms, err := ParseSymbols(raw); err == nil {
			s.ObservationSymbols = syms
		} else {
			slog.Warn("watchsettings: ignoring the stored observation symbols", "error", err)
		}
	}
	if raw, ok := read(ctx, store, KeyObservationDailyCostCapUSD); ok {
		if c, err := ParseCap(raw); err == nil {
			s.ObservationCapUSD = c
		} else {
			slog.Warn("watchsettings: using the default observation cost cap", "error", err)
		}
	}
	return s
}

func read(ctx context.Context, store Store, key string) (string, bool) {
	raw, ok, err := store.Get(ctx, key)
	if err != nil {
		slog.Warn("watchsettings: read runtime setting", "key", key, "error", err)
		return "", false
	}
	return raw, ok
}
