package settings

import (
	"context"
	"log/slog"

	"github.com/ousiassllc/pitha-trador/internal/config/dailyguardsettings"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/settings/derivedpanel"
	"github.com/ousiassllc/pitha-trador/internal/web/molecules"
)

// riskReferenceSource is the optional capability of an OperationalSettings
// that knows the Paper limits in force (*opsettings.Service, via
// WithRiskReference). Without it - or with ok false - the 整合チェック is
// skipped. It is reference data only: nothing here changes the risk engine.
type riskReferenceSource interface {
	RiskReference(ctx context.Context) (ref dailyguardsettings.RiskReference, ok bool, err error)
}

// dailyGuardDerived reads the effective daily entry-halt configuration for
// the group's 計算結果 panel. A read failure is logged and the panel says so
// (the rows themselves still render) rather than failing the Settings screen
// or showing values that may be stale.
func (h *SettingsHandler) dailyGuardDerived(ctx context.Context) []molecules.SettingDerivedItem {
	resolved, err := h.ops.DailyGuard(ctx)
	if err != nil {
		slog.Error("settings: read daily guard configuration; showing the derived panel as unreadable", "error", err)
		return []molecules.SettingDerivedItem{{Key: "error", Label: "計算結果を読み取れませんでした", Off: true}}
	}
	items := derivedpanel.DailyGuardItems(resolved)
	if ref, ok := h.riskReference(ctx); ok {
		items = append(items, derivedpanel.ConsistencyItems(dailyguardsettings.CheckConsistency(resolved, ref))...)
	}
	return append(items, derivedpanel.LiveReadinessItems(dailyguardsettings.LiveReadiness(resolved))...)
}

// riskReference reads what the 整合チェック compares the daily guard with; ok
// is false when it is unavailable (not wired, or a read failure, which is
// logged), which skips the check.
func (h *SettingsHandler) riskReference(ctx context.Context) (ref dailyguardsettings.RiskReference, ok bool) {
	src, wired := h.ops.(riskReferenceSource)
	if !wired {
		return dailyguardsettings.RiskReference{}, false
	}
	ref, ok, err := src.RiskReference(ctx)
	if err != nil {
		slog.Error("settings: read Paper risk limits for the consistency check; skipping it", "error", err)
		return dailyguardsettings.RiskReference{}, false
	}
	return ref, ok
}
