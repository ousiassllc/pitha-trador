package opsettings

import (
	"context"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/config/dailyguardsettings"
)

// DailyGuard is the effective daily entry-halt configuration (FR-RISK-8):
// the operating capital, how each yen threshold is derived from it (or given
// directly) and the Limits the risk engine reads. The Settings screen shows
// it as the 計算結果 panel.
func (s *Service) DailyGuard(ctx context.Context) (dailyguardsettings.Resolved, error) {
	return dailyguardsettings.LoadResolved(ctx, s.store)
}

// WithRiskReference records the risk engine's effective limits (the active
// mode's risk.yaml section) so the Settings screen can compare the daily
// guard with initial_capital and max_daily_loss_pct. It is read-only
// reference data: nothing here changes what the engine does.
func (s *Service) WithRiskReference(limits config.RiskLimits) *Service {
	s.riskReference = &dailyguardsettings.RiskReference{InitialCapitalJPY: limits.InitialCapital, MaxDailyLossPct: limits.MaxDailyLossPct}
	return s
}

// RiskReference returns the reference set by WithRiskReference; ok is false
// without one, which skips the consistency check.
func (s *Service) RiskReference() (ref dailyguardsettings.RiskReference, ok bool) {
	if s.riskReference == nil {
		return dailyguardsettings.RiskReference{}, false
	}
	return *s.riskReference, true
}
