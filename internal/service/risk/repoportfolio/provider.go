// Package repoportfolio is the production risk.PortfolioProvider: the
// exposure, daily-loss and loss-streak state Risk Engine's FR-RISK-1
// limits and FR-RISK-2 Kill Switch triggers need, derived from the
// positions table against the configured account-equity baseline
// (config/risk.yaml initial_capital). It lives beside internal/service/risk
// (which defines the interface) so neither package outgrows linterly's
// per-directory limit.
package repoportfolio

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository"
)

// recentClosedPositionsLimit bounds how many of the most-recently-opened
// positions.List rows Provider.recentClosedPositions
// scans to find the most-recently-closed ones (List has no closed_at-
// ordered query of its own - ListClosedBetween needs a [start,end) range,
// not "most recent N" - so this reads a generously large recent window
// and filters/sorts client-side). 200 comfortably covers FR-RISK-1's
// max_consecutive_losses (Paper/Live: 4/3) and cooldown checks even on an
// unusually active trading day.
const recentClosedPositionsLimit = 200

// ErrNoInitialCapital is returned by Provider's
// exposure/daily-loss methods when no account-equity baseline
// (config.RiskLimits.InitialCapital) is configured: Risk Engine's Check
// treats it as a read failure and rejects new trades (fail closed) rather
// than measuring "percentage of account equity" against nothing.
var ErrNoInitialCapital = errors.New("risk: initial_capital is not configured (config/risk.yaml)")

// tradingDayZone is the zone whose calendar day bounds "today" for
// DailyLossPct: TSE trades in JST (functional.md §4.7 max_daily_loss_pct).
var tradingDayZone = time.FixedZone("JST", 9*60*60)

// Provider is the real PortfolioProvider backed by
// internal/repository.PositionRepository (issue #48).
//
// TotalExposurePct/SymbolExposurePct/DailyLossPct measure against the
// account-equity baseline initialCapital (config/risk.yaml's
// initial_capital, the same value Execution's position sizing uses); with
// no baseline they return ErrNoInitialCapital.
type Provider struct {
	positions      *repository.PositionRepository
	initialCapital float64
	now            func() time.Time
}

// Option configures New.
type Option func(*Provider)

// WithClock overrides the clock DailyLossPct uses to find "today".
func WithClock(now func() time.Time) Option {
	return func(p *Provider) { p.now = now }
}

// New returns a Provider
// backed by positions, measuring exposure/loss against initialCapital
// (JPY).
func New(positions *repository.PositionRepository, initialCapital float64, opts ...Option) *Provider {
	p := &Provider{positions: positions, initialCapital: initialCapital, now: time.Now}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

func (p *Provider) OpenPositionCount(ctx context.Context) (int, error) {
	open, err := p.positions.ListOpen(ctx)
	if err != nil {
		return 0, err
	}
	return len(open), nil
}

// positionNotional is a position's current market value: quantity at the
// latest mark, or at entry until the first mark arrives.
func positionNotional(pos domain.Position) float64 {
	price := pos.CurrentPrice
	if price <= 0 {
		price = pos.EntryPrice
	}
	return float64(pos.Quantity) * price
}

func (p *Provider) TotalExposurePct(ctx context.Context) (float64, error) {
	if p.initialCapital <= 0 {
		return 0, ErrNoInitialCapital
	}
	open, err := p.positions.ListOpen(ctx)
	if err != nil {
		return 0, err
	}
	var notional float64
	for _, pos := range open {
		notional += positionNotional(pos)
	}
	return notional / p.initialCapital * 100, nil
}

func (p *Provider) SymbolExposurePct(ctx context.Context, instrumentID int64) (float64, error) {
	if p.initialCapital <= 0 {
		return 0, ErrNoInitialCapital
	}
	open, err := p.positions.ListOpen(ctx)
	if err != nil {
		return 0, err
	}
	var notional float64
	for _, pos := range open {
		if pos.InstrumentID == instrumentID {
			notional += positionNotional(pos)
		}
	}
	return notional / p.initialCapital * 100, nil
}

// DailyLossPct is today's (JST) net loss as a percentage of initialCapital:
// the realized P&L of positions closed today plus the unrealized P&L of
// every position still open. Negative when today is net profitable.
func (p *Provider) DailyLossPct(ctx context.Context) (float64, error) {
	if p.initialCapital <= 0 {
		return 0, ErrNoInitialCapital
	}
	now := p.now().In(tradingDayZone)
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, tradingDayZone)
	closed, err := p.positions.ListClosedBetween(ctx, dayStart, dayStart.AddDate(0, 0, 1))
	if err != nil {
		return 0, err
	}
	open, err := p.positions.ListOpen(ctx)
	if err != nil {
		return 0, err
	}
	var pnl float64
	for _, pos := range closed {
		if pos.RealizedPnL != nil {
			pnl += *pos.RealizedPnL
		}
	}
	for _, pos := range open {
		pnl += pos.UnrealizedPnL
	}
	return -pnl / p.initialCapital * 100, nil
}

// recentClosedPositions returns positions.List's recentClosedPositionsLimit
// most-recently-opened rows, filtered to closed ones only and sorted
// most-recently-closed first.
func (p *Provider) recentClosedPositions(ctx context.Context) ([]closedPosition, error) {
	rows, err := p.positions.List(ctx, recentClosedPositionsLimit)
	if err != nil {
		return nil, err
	}

	closed := make([]closedPosition, 0, len(rows))
	for _, row := range rows {
		if row.ClosedAt == nil || row.RealizedPnL == nil {
			continue
		}
		closed = append(closed, closedPosition{closedAt: *row.ClosedAt, realizedPnL: *row.RealizedPnL})
	}
	slices.SortFunc(closed, func(a, b closedPosition) int { return b.closedAt.Compare(a.closedAt) })
	return closed, nil
}

type closedPosition struct {
	closedAt    time.Time
	realizedPnL float64
}

func (p *Provider) ConsecutiveLosses(ctx context.Context) (int, error) {
	closed, err := p.recentClosedPositions(ctx)
	if err != nil {
		return 0, err
	}

	count := 0
	for _, pos := range closed {
		if pos.realizedPnL >= 0 {
			break
		}
		count++
	}
	return count, nil
}

func (p *Provider) LastLossAt(ctx context.Context) (time.Time, error) {
	closed, err := p.recentClosedPositions(ctx)
	if err != nil {
		return time.Time{}, err
	}

	for _, pos := range closed {
		if pos.realizedPnL < 0 {
			return pos.closedAt, nil
		}
	}
	return time.Time{}, nil
}
