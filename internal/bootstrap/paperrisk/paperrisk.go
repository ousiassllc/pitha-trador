// Package paperrisk is the composition-root glue of the Paper operating
// capital and exposure limits the Settings screen edits (issue #804,
// internal/config/papersettings): the start-up seeding and the per-call
// capital readers the Risk Engine's portfolio, the updater gate and the
// Performance evaluation use. Paper only: a Live Engine is built without any
// of it and keeps using config/risk.yaml.
package paperrisk

import (
	"context"
	"log/slog"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/config/papersettings"
)

// Seed stores the Paper initial capital and exposure limits (500,000 円 / 100%
// / 100%, papersettings.SeedDefaults) once per database. A failure is logged
// and never stops startup: the config/risk.yaml Paper values then apply.
//
// Premise: this build is Paper-only. Gate the call on Paper before any Live
// build is added.
func Seed(ctx context.Context, store papersettings.SeedStore, now time.Time) {
	if err := papersettings.SeedDefaults(ctx, store, now); err != nil {
		slog.Warn("paperrisk: seed Paper risk defaults failed; the config/risk.yaml Paper values apply", "error", err)
	}
}

// CapitalSource reads the Paper operating capital in force now: base's
// (config/risk.yaml) unless the Settings screen stored one. It is
// repoportfolio.WithCapitalSource's argument.
func CapitalSource(store papersettings.Store, base config.RiskLimits) func(ctx context.Context) (float64, error) {
	return func(ctx context.Context) (float64, error) {
		limits, err := papersettings.Effective(ctx, store, base)
		return limits.InitialCapital, err
	}
}

// Limits is the part of the Risk Engine CapitalNow needs.
type Limits interface {
	EffectiveLimits(ctx context.Context) (config.RiskLimits, error)
}

// CapitalNow returns the Paper operating capital in force at the time of each
// call; 0 when it cannot be read, which the callers treat as "capital unset".
func CapitalNow(engine Limits) func(ctx context.Context) float64 {
	return func(ctx context.Context) float64 {
		limits, err := engine.EffectiveLimits(ctx)
		if err != nil {
			slog.WarnContext(ctx, "paperrisk: read Paper operating capital failed", "error", err)
			return 0
		}
		return limits.InitialCapital
	}
}
