package execution

import (
	"context"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
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
	position, err := e.positions.Get(ctx, positionID)
	if err != nil {
		return domain.Position{}, fmt.Errorf("execution: get position %d: %w", positionID, err)
	}
	if !position.IsOpen() {
		return domain.Position{}, fmt.Errorf("execution: position %d is already closed", positionID)
	}

	exitSide := domain.OrderSideSell
	if position.Side == domain.PositionSideShort {
		exitSide = domain.OrderSideBuy
	}

	exitOrder, err := e.orders.Insert(ctx, domain.PaperOrder{
		InstrumentID: position.InstrumentID,
		Symbol:       position.Symbol,
		Side:         exitSide,
		OrderType:    domain.OrderTypeMarket,
		Quantity:     position.Quantity,
		Status:       domain.OrderStatusPending,
		SubmittedAt:  now,
	})
	if err != nil {
		return domain.Position{}, fmt.Errorf("execution: submit exit order for position %d: %w", positionID, err)
	}
	if _, err := e.orders.Fill(ctx, exitOrder.ID, exitPrice, nil, now); err != nil {
		return domain.Position{}, fmt.Errorf("execution: fill exit order %d for position %d: %w", exitOrder.ID, positionID, err)
	}

	realizedPnL := positionSign(position.Side) * float64(position.Quantity) * (exitPrice - position.EntryPrice)

	closed, err := e.positions.Close(ctx, positionID, exitOrder.ID, exitPrice, realizedPnL, reason, now)
	if err != nil {
		return domain.Position{}, fmt.Errorf("execution: close position %d: %w", positionID, err)
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
