package opsettings

import (
	"context"

	"github.com/ousiassllc/pitha-trador/internal/config/dailyguardsettings"
)

// DailyGuard is the effective daily entry-halt configuration (FR-RISK-8):
// the operating capital, how each yen threshold is derived from it (or given
// directly) and the Limits the risk engine reads. The Settings screen shows
// it as the 計算結果 panel.
func (s *Service) DailyGuard(ctx context.Context) (dailyguardsettings.Resolved, error) {
	return dailyguardsettings.LoadResolved(ctx, s.store)
}
