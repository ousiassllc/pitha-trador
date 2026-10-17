package dailyguardreplay

import (
	"github.com/ousiassllc/pitha-trador/internal/config/dailyguardsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// Guard is one trading day's halt state, mirroring the risk engine's
// evaluateDailyGuard (internal/service/risk/dailyguard.go): the day's P&L peak
// (never below 0) trails the profit lock, and the first rule reached latches
// for the rest of the day.
type Guard struct {
	limits dailyguardsettings.Limits
	peak   float64
	reason string
}

// NewGuard returns a fresh day's state under limits.
func NewGuard(limits dailyguardsettings.Limits) *Guard { return &Guard{limits: limits} }

// Halted is the latched halt reason, "" while entries are allowed.
func (g *Guard) Halted() string { return g.reason }

// Update feeds the day's P&L (yen) and opened-trade count and returns the
// halt reason: the latched one once any rule has been reached.
func (g *Guard) Update(pnl float64, trades int) string {
	if g.reason != "" {
		return g.reason
	}
	if pnl > g.peak {
		g.peak = pnl
	}
	g.reason = breach(g.limits, pnl, trades, g.peak)
	return g.reason
}

// breach mirrors risk.dailyGuardBreach's conditions and order: loss limit,
// profit lock, trade count.
func breach(l dailyguardsettings.Limits, pnl float64, trades int, peak float64) string {
	switch {
	case l.LossLimitEnabled() && pnl <= -l.LossLimitJPY:
		return domain.DailyHaltReasonLossLimit
	case l.ProfitLockEnabled() && peak >= l.ProfitLockTriggerJPY && pnl <= peak-peak*l.ProfitLockGivebackPct/100:
		return domain.DailyHaltReasonProfitLock
	case l.MaxTradesEnabled() && trades >= l.MaxTrades:
		return domain.DailyHaltReasonMaxTrades
	}
	return ""
}
