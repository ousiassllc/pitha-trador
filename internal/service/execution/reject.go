package execution

import (
	"context"
	"log/slog"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

const (
	// rejectTimeout bounds a REJECTED write issued after the caller's ctx
	// may already be cancelled.
	rejectTimeout = 5 * time.Second
	// staleMarketOrderAfter is how long a MARKET entry order may sit PENDING
	// before it is treated as an orphan. A market order is inserted and
	// filled back to back (Enter), so a PENDING one this old can only be left
	// by a crash / unrecoverable failure between the two steps; nothing else
	// would ever resolve it (only LIMIT orders are retried by OnSnapshot).
	staleMarketOrderAfter = time.Minute
)

// rejectOrder sets order REJECTED detached from ctx's cancellation (bounded
// by rejectTimeout): the reasons an entry fails (shutdown cancelling ctx,
// "database is locked") must not also stop the clean-up, which would leave
// the order PENDING and block the instrument's later entries (#627).
func (e *Engine) rejectOrder(ctx context.Context, order domain.PaperOrder) error {
	rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), rejectTimeout)
	defer cancel()
	_, err := e.orders.UpdateStatus(rctx, order.ID, domain.OrderStatusRejected)
	return err
}

// isStalePendingMarket reports whether o is a PENDING MARKET order that
// has outlived staleMarketOrderAfter at now.
func isStalePendingMarket(o domain.PaperOrder, now time.Time) bool {
	return o.Status == domain.OrderStatusPending && o.OrderType == domain.OrderTypeMarket &&
		now.Sub(o.SubmittedAt) >= staleMarketOrderAfter
}

// rejectStaleMarketOrders REJECTs every orphaned PENDING market order in
// orders (see staleMarketOrderAfter) and returns the remaining orders, so
// callers neither treat an orphan as a live pending entry nor leave it for
// ever. A failed REJECT is logged and the order dropped from the result
// anyway: it is not a live order, and the next call retries the clean-up.
func (e *Engine) rejectStaleMarketOrders(ctx context.Context, orders []domain.PaperOrder, now time.Time) []domain.PaperOrder {
	live := orders[:0:0]
	for _, o := range orders {
		if !isStalePendingMarket(o, now) {
			live = append(live, o)
			continue
		}
		if err := e.rejectOrder(ctx, o); err != nil {
			slog.ErrorContext(ctx, "execution: reject orphaned pending market order", "symbol", o.Symbol, "order_id", o.ID, "error", err)
			continue
		}
		slog.WarnContext(ctx, "execution: rejected orphaned pending market order", "symbol", o.Symbol, "order_id", o.ID)
	}
	return live
}
