package execution

import (
	"context"
	"errors"
	"fmt"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// ErrOrderNotPending is returned by CancelPendingEntry when the order is not a
// PENDING one any more (it filled, was rejected or was already cancelled): it
// is left as it is.
var ErrOrderNotPending = errors.New("execution: order is not pending")

// CancelPendingEntry cancels a PENDING entry order (a limit order that has not
// filled yet), for a caller that gives up on it (an order proposal's expiry or
// cancellation, issue #828). It takes OnSnapshot's lock, the one fills happen
// under, so the check and the CANCELLED write cannot interleave with a fill: an
// order that fills first stays FILLED (ErrOrderNotPending) and is never turned
// CANCELLED under its open position.
func (e *Engine) CancelPendingEntry(ctx context.Context, orderID int64) error {
	e.snapshotMu.Lock()
	defer e.snapshotMu.Unlock()
	order, err := e.orders.Get(ctx, orderID)
	if err != nil {
		return fmt.Errorf("execution: get order %d to cancel: %w", orderID, err)
	}
	if order.Status != domain.OrderStatusPending {
		return ErrOrderNotPending
	}
	if _, err := e.orders.UpdateStatus(ctx, orderID, domain.OrderStatusCancelled); err != nil {
		return fmt.Errorf("execution: cancel order %d: %w", orderID, err)
	}
	return nil
}
