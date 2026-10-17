package simrisk

import (
	"context"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/risk"
	"github.com/ousiassllc/pitha-trador/internal/service/risk/correlation"
	"github.com/ousiassllc/pitha-trador/internal/service/risk/sizing"
)

// tradingDay bounds a trading day: TSE trades in JST (like risk's own).
var tradingDay = time.FixedZone("JST", 9*60*60)

// Config is what the simulated Risk Engine needs: the limits (config/risk.yaml
// paper section in production) and the stop-loss percentage sizing is
// measured against (execution.Config.StopLossPct; 0 uses
// sizing.DefaultStopLossPct, as live does).
type Config struct {
	Limits      config.RiskLimits
	StopLossPct float64
}

type position struct {
	side      string
	quantity  int64
	entry     float64
	lastPrice float64
}

// notional is live's positionNotional: quantity at the last mark.
func (p position) notional() float64 { return float64(p.quantity) * p.lastPrice }

func (p position) unrealized() float64 {
	sign := 1.0
	if p.side == domain.PositionSideShort {
		sign = -1
	}
	return sign * float64(p.quantity) * (p.lastPrice - p.entry)
}

type closed struct {
	at  time.Time
	pnl float64
}

// Portfolio is the simulated account state and implements
// policy.RiskChecker. It is single-goroutine state: the replay sets the clock
// and the candidate bar (Observe, Candidate) right before the Policy Engine
// asks Check.
type Portfolio struct {
	cfg  Config
	open map[int64]*position

	now time.Time
	day string // trading day of now ("2006-01-02" JST)

	// closedToday are the day's closed trades (realized P&L of the daily-loss
	// limit); streak are the closed trades since the loss-streak baseline,
	// oldest first (consecutive losses, cooldown).
	closedToday    []closed
	streak         []closed
	streakBaseline time.Time

	// killed is the reason of the Kill Switch the replay raised today
	// (daily loss or consecutive losses), "" while none: like live it stays
	// in force for the rest of the trading day, even if the loss recovers.
	killed string

	candidate domain.Snapshot
}

// New returns an empty Portfolio.
func New(cfg Config) *Portfolio {
	if cfg.StopLossPct <= 0 {
		cfg.StopLossPct = sizing.DefaultStopLossPct
	}
	return &Portfolio{cfg: cfg, open: map[int64]*position{}}
}

// Advance moves the replay clock to now. On a new trading day the day's
// realized P&L restarts and a Kill Switch raised on a previous day is lifted
// by the assumed operator resume (see the package doc): the loss streak that
// raised one restarts there.
func (p *Portfolio) Advance(now time.Time) {
	day := now.In(tradingDay).Format("2006-01-02")
	if day != p.day {
		p.day = day
		p.closedToday = nil
		if p.killed != "" {
			p.killed = ""
			p.streakBaseline = now
			p.streak = nil
		}
	}
	p.now = now
}

// raiseKillSwitch latches the daily_loss_limit / consecutive_losses Kill
// Switch when the replayed state has reached either limit (live raises them
// from Check and every minute from RunPeriodicChecks).
func (p *Portfolio) raiseKillSwitch() {
	if p.killed != "" {
		return
	}
	lim := p.cfg.Limits
	if loss := p.dailyLossPct(); loss >= lim.MaxDailyLossPct {
		p.killed = fmt.Sprintf("%s: daily_loss_pct=%.4f max=%.4f", risk.ReasonMaxDailyLossPct, loss, lim.MaxDailyLossPct)
	} else if losses := p.consecutiveLosses(); losses >= lim.MaxConsecutiveLosses {
		p.killed = fmt.Sprintf("%s: consecutive_losses=%d max=%d", risk.ReasonMaxConsecutiveLosses, losses, lim.MaxConsecutiveLosses)
	}
}

// Observe marks instrumentID's open position, if any, at price (live marks
// every position on each snapshot of its instrument).
func (p *Portfolio) Observe(instrumentID int64, price float64) {
	if pos := p.open[instrumentID]; pos != nil {
		pos.lastPrice = price
		p.raiseKillSwitch()
	}
}

// Candidate sets the bar the next Check evaluates (live reads the instrument's
// latest snapshot for the spread, the market return and the sizing price).
func (p *Portfolio) Candidate(bar domain.Snapshot) { p.candidate = bar }

// Size is the order quantity for an entry at price (live's PositionSize); 0
// means no lot fits the limits.
func (p *Portfolio) Size(price float64) int64 {
	qty, _ := sizing.Quantity(p.cfg.Limits, p.cfg.StopLossPct, price, p.totalExposurePct())
	return qty
}

// Open records a new position.
func (p *Portfolio) Open(instrumentID int64, side string, quantity int64, entryPrice float64) {
	p.open[instrumentID] = &position{side: side, quantity: quantity, entry: entryPrice, lastPrice: entryPrice}
}

// Close removes instrumentID's position and records its realized pnl (JPY)
// at at.
func (p *Portfolio) Close(instrumentID int64, at time.Time, pnl float64) {
	delete(p.open, instrumentID)
	c := closed{at: at, pnl: pnl}
	p.closedToday = append(p.closedToday, c)
	p.streak = append(p.streak, c)
	p.raiseKillSwitch()
}

// OpenCount is the number of open positions.
func (p *Portfolio) OpenCount() int { return len(p.open) }

// OpenCountBySide is the number of open positions on side.
func (p *Portfolio) OpenCountBySide(side string) int {
	n := 0
	for _, pos := range p.open {
		if pos.side == side {
			n++
		}
	}
	return n
}

func (p *Portfolio) totalExposurePct() float64 {
	var notional float64
	for _, pos := range p.open {
		notional += pos.notional()
	}
	return notional / p.cfg.Limits.InitialCapital * 100
}

func (p *Portfolio) symbolExposurePct(instrumentID int64) float64 {
	if pos := p.open[instrumentID]; pos != nil {
		return pos.notional() / p.cfg.Limits.InitialCapital * 100
	}
	return 0
}

// dailyLossPct is live's DailyLossPct: today's realized P&L plus the open
// positions' unrealized P&L as a percentage loss of the initial capital.
func (p *Portfolio) dailyLossPct() float64 {
	var pnl float64
	for _, c := range p.closedToday {
		pnl += c.pnl
	}
	for _, pos := range p.open {
		pnl += pos.unrealized()
	}
	return -pnl / p.cfg.Limits.InitialCapital * 100
}

// consecutiveLosses counts the losing trades since the baseline, newest
// first, until the first winner.
func (p *Portfolio) consecutiveLosses() int {
	n := 0
	for i := len(p.streak) - 1; i >= 0 && p.streak[i].pnl < 0; i-- {
		n++
	}
	return n
}

func (p *Portfolio) lastLossAt() time.Time {
	for i := len(p.streak) - 1; i >= 0; i-- {
		if p.streak[i].pnl < 0 {
			return p.streak[i].at
		}
	}
	return time.Time{}
}

// Check implements policy.RiskChecker with risk.Engine.Check's rule order
// and reasons: Kill Switch, cooldown, open positions, same direction,
// exposure, daily loss, consecutive losses, spread, market adverse, then
// sizing.
func (p *Portfolio) Check(_ context.Context, instrumentID int64, direction string) (bool, string) {
	lim := p.cfg.Limits
	if p.killed != "" {
		return false, p.killed
	}
	if lastLoss := p.lastLossAt(); !lastLoss.IsZero() {
		if resumeAt := lastLoss.Add(time.Duration(lim.CooldownAfterLossMinutes) * time.Minute); p.now.Before(resumeAt) {
			return false, fmt.Sprintf("%s: retry_after=%s", risk.ReasonCooldownAfterLoss, resumeAt.Format(time.RFC3339))
		}
	}
	if count := len(p.open); count >= lim.MaxOpenPositions {
		return false, fmt.Sprintf("%s: count=%d max=%d", risk.ReasonMaxOpenPositions, count, lim.MaxOpenPositions)
	}
	sameDirection := p.OpenCountBySide(direction)
	if correlation.SameDirectionExceeded(lim, sameDirection) {
		return false, fmt.Sprintf("%s: direction=%s count=%d max=%d", risk.ReasonMaxSameDirection, direction, sameDirection, lim.MaxSameDirectionPositions)
	}
	totalPct := p.totalExposurePct()
	if totalPct >= lim.MaxTotalExposurePct {
		return false, fmt.Sprintf("%s: exposure_pct=%.4f max=%.4f", risk.ReasonMaxTotalExposurePct, totalPct, lim.MaxTotalExposurePct)
	}
	if symbolPct := p.symbolExposurePct(instrumentID); symbolPct >= lim.MaxPositionPerSymbolPct {
		return false, fmt.Sprintf("%s: exposure_pct=%.4f max=%.4f", risk.ReasonMaxPositionPerSymbolPct, symbolPct, lim.MaxPositionPerSymbolPct)
	}
	p.raiseKillSwitch()
	if p.killed != "" {
		return false, p.killed
	}

	bar := p.candidate
	if bar.SpreadBps == nil {
		return false, fmt.Sprintf("%s: spread_bps missing (data_missing)", risk.ReasonRiskEngineError)
	}
	if *bar.SpreadBps > lim.MaxSpreadBps {
		return false, fmt.Sprintf("%s: spread_bps=%.2f max=%.2f", risk.ReasonMaxSpreadBps, *bar.SpreadBps, lim.MaxSpreadBps)
	}
	if pct, adverse := correlation.MarketAdverse(lim, direction, sameDirection, bar.Feature.MarketReturn5m); adverse {
		return false, fmt.Sprintf("%s: direction=%s same_direction_positions=%d market_return_5m_pct=%.4f threshold_pct=%.4f", risk.ReasonMarketAdverse, direction, sameDirection, pct, lim.MarketAdverseReturn5mPct)
	}
	if qty, reason := sizing.Quantity(lim, p.cfg.StopLossPct, bar.Price, totalPct); qty == 0 {
		return false, fmt.Sprintf("%s: price=%.2f stop_loss_pct=%.2f max_trade_loss_pct=%.4f", reason, bar.Price, p.cfg.StopLossPct, lim.MaxTradeLossPct)
	}
	return true, ""
}
