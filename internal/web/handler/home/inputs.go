package home

import (
	"context"
	"log/slog"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/dailyguardsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/activityfeed"
	"github.com/ousiassllc/pitha-trador/internal/service/opsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/updater"
	"github.com/ousiassllc/pitha-trador/internal/web/organisms"
)

// The narrow views of the existing services the Home screen reads. Each is a
// subset of what internal/router already wires for the header routes and the
// APIs (`/api/v1/system/status`, `/system/daily-halt`, `/system/marketdata-status`,
// `/system/health-alerts`, `/api/v1/scanner/scan`,
// `/api/v1/activity`); Home adds no source of its own and writes nothing.
type (
	// StateReader is `GET /api/v1/system/status`'s source.
	StateReader interface {
		State(ctx context.Context) (domain.SystemState, []domain.KillSwitchEvent, error)
	}
	// DailyHaltReader is `GET /system/daily-halt`'s source.
	DailyHaltReader interface {
		DailyHalt(ctx context.Context) (*domain.DailyEntryHalt, error)
	}
	// MarketDataReader is `GET /system/marketdata-status`'s content.
	MarketDataReader interface {
		BannerProps(ctx context.Context) organisms.MarketDataBannerProps
	}
	// AlertReader is `GET /system/health-alerts`'s source.
	AlertReader interface {
		Active(ctx context.Context) ([]domain.HealthAlert, error)
	}
	// SecretsReader lists the optional keys that have no value yet.
	SecretsReader interface {
		UnsetOptionalKeys(ctx context.Context) []string
	}
	// UpdateReader is the update notification's source.
	UpdateReader interface {
		Status() updater.Status
	}
	// ScanReader is the Scanner's scan view source (internal/web/handler/scanner.ScanSource).
	ScanReader interface {
		Scan(ctx context.Context) (cycle domain.ScanCycle, ok bool, err error)
	}
	// SettingsReader reads the effective operational settings (the minimum
	// entry quality, the daily loss limit).
	SettingsReader interface {
		Get(ctx context.Context, key string) (opsettings.Value, error)
		DailyGuard(ctx context.Context) (dailyguardsettings.Resolved, error)
	}
	// ActivityReader is `GET /api/v1/activity`'s source.
	ActivityReader interface {
		Snapshot(ctx context.Context, q activityfeed.Query) (domain.ActivitySnapshot, error)
	}
)

// statusInputs are the reads the 取引の状態 card, the 要対応 list and the
// header badge share. A nil dependency is a source this build does not have
// (a router-level test): its input stays at the zero value without an error.
// A failed read is kept in its own *Err field; nothing else is affected.
type statusInputs struct {
	now time.Time

	stateKnown bool
	state      domain.SystemState
	events     []domain.KillSwitchEvent
	stateErr   error

	halt    *domain.DailyEntryHalt
	haltErr error

	market organisms.MarketDataBannerProps

	alerts    []domain.HealthAlert
	alertsErr error

	health    Health
	healthErr error

	update    updater.Status
	unsetKeys []string
}

// loadStatus reads the shared inputs once.
func (h *Handler) loadStatus(ctx context.Context, now time.Time) statusInputs {
	in := statusInputs{now: now}
	if h.deps.State != nil {
		in.stateKnown = true
		in.state, in.events, in.stateErr = h.deps.State.State(ctx)
		logRead(ctx, "system state", in.stateErr)
	}
	if h.deps.DailyHalt != nil {
		in.halt, in.haltErr = h.deps.DailyHalt.DailyHalt(ctx)
		logRead(ctx, "daily halt", in.haltErr)
	}
	if h.deps.MarketData != nil {
		in.market = h.deps.MarketData.BannerProps(ctx)
	}
	if h.deps.Alerts != nil {
		in.alerts, in.alertsErr = h.deps.Alerts.Active(ctx)
		logRead(ctx, "health alerts", in.alertsErr)
	}
	in.health, in.healthErr = h.health(ctx, now, healthBasis(in))
	logRead(ctx, "health today", in.healthErr)
	if h.deps.Update != nil {
		in.update = h.deps.Update.Status()
	}
	if h.deps.Secrets != nil {
		in.unsetKeys = h.deps.Secrets.UnsetOptionalKeys(ctx)
	}
	return in
}

// logRead logs a failed read; the cause stays in the log and the card only
// says that the read failed.
func logRead(ctx context.Context, what string, err error) {
	if err != nil {
		slog.ErrorContext(ctx, "handler: home read failed", "read", what, "error", err)
	}
}
