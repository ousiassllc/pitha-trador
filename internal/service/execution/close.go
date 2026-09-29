package execution

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/risk"
)

// Close implements Paper Exit (FR-EXIT-1〜2): submits a market exit order
// opposite position's side, fills it immediately at exitPrice, and closes
// position with the resulting realized P&L and reason. reason is one of
// the domain.ExitReason* constants (domain.ExitReasonManual for
// operator-initiated `POST /positions/:id/close`).
//
// A losing close (realizedPnL < 0) starts this symbol's
// Config.CooldownAfterLossMinutes cooldown (functional.md §4.9
// cooldown_until) - an in-memory, per-symbol gate Enter checks, distinct
// from internal/service/risk.Engine's own portfolio-wide
// cooldown_after_loss_minutes check (config.go's doc comment).
func (e *Engine) Close(ctx context.Context, positionID int64, reason string, exitPrice float64, now time.Time) (domain.Position, error) {
	e.closeMu.Lock()
	defer e.closeMu.Unlock()
	position, err := e.positions.Get(ctx, positionID)
	if err != nil {
		return domain.Position{}, fmt.Errorf("execution: get position %d: %w", positionID, err)
	}
	if !position.IsOpen() {
		return domain.Position{}, fmt.Errorf("%w: position %d", domain.ErrPositionAlreadyClosed, positionID)
	}

	exitSide := domain.OrderSideSell
	if position.Side == domain.PositionSideShort {
		exitSide = domain.OrderSideBuy
	}
	realizedPnL := positionSign(position.Side) * float64(position.Quantity) * (exitPrice - position.EntryPrice)

	// Submit, fill and close in one transaction: no orphan FILLED exit order.
	closed, err := e.positions.CloseWithExitOrder(ctx, domain.PaperOrder{
		InstrumentID: position.InstrumentID,
		Symbol:       position.Symbol,
		Side:         exitSide,
		OrderType:    domain.OrderTypeMarket,
		Quantity:     position.Quantity,
		Status:       domain.OrderStatusPending,
		SubmittedAt:  now,
	}, exitPrice, positionID, realizedPnL, reason, now)
	if errors.Is(err, domain.ErrPositionNotFound) {
		return domain.Position{}, fmt.Errorf("%w: position %d", domain.ErrPositionAlreadyClosed, positionID)
	}
	if err != nil {
		return domain.Position{}, fmt.Errorf("execution: exit position %d: %w", positionID, err)
	}

	if realizedPnL < 0 {
		e.startCooldown(closed.Symbol, now)
	}
	return closed, nil
}

// positionSign returns +1 for a LONG position (profits as price rises) or
// -1 for SHORT (profits as price falls).
func positionSign(side string) float64 {
	if side == domain.PositionSideShort {
		return -1
	}
	return 1
}

// CloseAll implements internal/service/risk.PositionCloser (FR-RISK-3):
// force-closes every currently open position when Kill Switch fires for
// reason. Unlike Close, it does not take a caller-supplied exit price -
// each position closes at its own last known CurrentPrice (the most
// recent mark-to-market update, functional.md §4.10 FR-SCHED-4's
// periodic re-evaluation), matching Execution's existing reliance on
// caller-supplied/stored prices rather than an internal live-quote
// source (exitrule.go's MarketContext). It attempts every open position
// even if one fails, joining every resulting error together rather than
// stopping at the first one - a stuck position must never block the
// others from closing.
func (e *Engine) CloseAll(ctx context.Context, reason string) error {
	open, err := e.positions.ListOpen(ctx)
	if err != nil {
		return fmt.Errorf("execution: list open positions for kill switch close (%s): %w", reason, err)
	}
	now := e.cfg.Now()
	var errs []error
	for _, position := range open {
		if _, err := e.Close(ctx, position.ID, domain.ExitReasonForceClose, position.CurrentPrice, now); err != nil && !errors.Is(err, domain.ErrPositionAlreadyClosed) {
			errs = append(errs, fmt.Errorf("execution: force-close position %d for kill switch %q: %w", position.ID, reason, err))
		}
	}
	return errors.Join(errs...)
}

// var _ risk.PositionCloser assertion below keeps CloseAll's signature
// pinned to the interface it exists to satisfy (internal/bootstrap passes
// an *Engine as risk.Config.Closer), so a signature drift surfaces here
// rather than at that distant call site.
var _ risk.PositionCloser = (*Engine)(nil)
