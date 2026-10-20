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

// WithRiskReference records the risk engine's limits (the active mode's
// risk.yaml section) so the Settings screen can compare the daily guard with
// the Paper operating capital and max_daily_loss_pct, and so the Paper
// capital / exposure settings (papersettings) know their risk.yaml fallback.
// It is read-only reference data: nothing here changes what the engine does.
func (s *Service) WithRiskReference(limits config.RiskLimits) *Service {
	s.riskReference = &dailyguardsettings.RiskReference{InitialCapitalJPY: limits.InitialCapital, MaxDailyLossPct: limits.MaxDailyLossPct}
	s.paperBase = &limits
	return s
}

// RiskReference returns what the daily guard's consistency check compares
// with: max_daily_loss_pct from risk.yaml and the Paper operating capital in
// force now (the Settings value over risk.yaml's initial_capital, issue
// #804). ok is false without WithRiskReference, which skips the check.
func (s *Service) RiskReference(ctx context.Context) (ref dailyguardsettings.RiskReference, ok bool, err error) {
	if s.riskReference == nil {
		return dailyguardsettings.RiskReference{}, false, nil
	}
	limits, err := s.PaperLimits(ctx)
	if err != nil {
		return dailyguardsettings.RiskReference{}, false, err
	}
	ref = *s.riskReference
	ref.InitialCapitalJPY = limits.InitialCapital
	return ref, true, nil
}
