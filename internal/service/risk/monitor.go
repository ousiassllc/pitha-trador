package risk

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository"
)

// DefaultFailureThreshold is Config.FailureThreshold's default: FR-RISK-2's
// "一定回数継続" for broker_api_error/db_write_failure.
const DefaultFailureThreshold = 5

// priceEpsilon is the tolerance for comparing a filled price with the
// price recorded on the position it opened (both are stored REST floats,
// so any real difference is far larger).
const priceEpsilon = 1e-6

// FailureCounter reports how many consecutive failures a dependency has
// had. *domain.FailureStreak implements it; the dependency's own package
// records each outcome, Engine only reads it.
type FailureCounter interface {
	ConsecutiveFailures() int
}

// RunPeriodicChecks runs every FR-RISK-2 detector and the §5.2 daily-loss
// warning once. internal/service/scheduler.Scheduler calls it every
// minute (WithRiskMonitor); every check runs even if an earlier one
// fails, and the failures are joined.
func (e *Engine) RunPeriodicChecks(ctx context.Context) error {
	return errors.Join(
		e.CheckMarketDataHealth(ctx),
		e.CheckJevAPIHealth(ctx),
		e.CheckBrokerAPIHealth(ctx),
		e.CheckDBWriteHealth(ctx),
		e.CheckPositionReconciliation(ctx),
		e.CheckDailyLossWarning(ctx),
	)
}

// CheckBrokerAPIHealth raises a broker_api_error Kill Switch (FR-RISK-2
// "Broker API異常", manual-resume-only per FR-RISK-7) once
// Config.BrokerAPIFailures has reached FailureThreshold consecutive
// failures.
func (e *Engine) CheckBrokerAPIHealth(ctx context.Context) error {
	return e.checkFailureStreak(ctx, domain.KillReasonBrokerAPIError, e.brokerAPIFailures)
}

// CheckDBWriteHealth raises a db_write_failure Kill Switch (FR-RISK-2
// "DB書き込み失敗が一定回数継続", manual-resume-only) once
// Config.DBWriteFailures has reached FailureThreshold consecutive failures.
func (e *Engine) CheckDBWriteHealth(ctx context.Context) error {
	return e.checkFailureStreak(ctx, domain.KillReasonDBWriteFailure, e.dbWriteFailures)
}

func (e *Engine) checkFailureStreak(ctx context.Context, reason string, counter FailureCounter) error {
	if counter == nil {
		return nil
	}
	failures := counter.ConsecutiveFailures()
	if failures < e.failureThreshold {
		return nil
	}
	_, err := e.triggerIfNotActive(ctx, reason, map[string]any{
		"consecutive_failures": failures, "threshold": e.failureThreshold,
	})
	return err
}

// CheckPositionReconciliation reconciles every open position against the
// order that opened it (FR-RISK-2 想定外ポジション発生 / 約定差異検知, both
// manual-resume-only): Paper Trading has no external Broker to compare
// against, so the fill record in paper_orders is the source of truth.
//
//   - unexpected_position: the entry order is missing, is not FILLED, or
//     contradicts the position's instrument or side (BUY <-> LONG,
//     SELL <-> SHORT) - a position no recorded fill explains.
//   - fill_discrepancy: the entry order was filled, but with a quantity or
//     price different from the position it opened, or (LIMIT) at a price
//     worse than its limit.
//
// It does nothing unless Config.Positions and Config.Orders are set.
func (e *Engine) CheckPositionReconciliation(ctx context.Context) error {
	if e.positions == nil || e.orders == nil {
		return nil
	}
	open, err := e.positions.ListOpen(ctx)
	if err != nil {
		return fmt.Errorf("risk: list open positions for reconciliation: %w", err)
	}
	for _, p := range open {
		reason, detail, err := e.reconcile(ctx, p)
		if err != nil {
			return err
		}
		if reason == "" {
			continue
		}
		detail["position_id"] = p.ID
		detail["symbol"] = p.Symbol
		if _, err := e.triggerIfNotActive(ctx, reason, detail); err != nil {
			return err
		}
		// One Kill Switch already latches (and force-closes) everything;
		// the remaining positions need no separate event.
		return nil
	}
	return nil
}

// reconcile returns the Kill Switch reason p's entry order contradicts
// (empty if consistent) with the detail to record.
func (e *Engine) reconcile(ctx context.Context, p domain.Position) (string, map[string]any, error) {
	order, err := e.orders.Get(ctx, p.EntryOrderID)
	if errors.Is(err, repository.ErrOrderNotFound) {
		return domain.KillReasonUnexpectedPosition, map[string]any{"problem": "entry_order_missing", "entry_order_id": p.EntryOrderID}, nil
	}
	if err != nil {
		return "", nil, fmt.Errorf("risk: get entry order %d for reconciliation: %w", p.EntryOrderID, err)
	}

	detail := map[string]any{"entry_order_id": order.ID}
	if order.Status != domain.OrderStatusFilled || order.FilledPrice == nil {
		detail["problem"] = "entry_order_not_filled"
		detail["order_status"] = order.Status
		return domain.KillReasonUnexpectedPosition, detail, nil
	}
	if order.InstrumentID != p.InstrumentID || !sideMatches(order.Side, p.Side) {
		detail["problem"] = "entry_order_mismatch"
		detail["order_side"] = order.Side
		detail["position_side"] = p.Side
		return domain.KillReasonUnexpectedPosition, detail, nil
	}

	filled := *order.FilledPrice
	switch {
	case order.Quantity != p.Quantity:
		detail["problem"] = "quantity_mismatch"
		detail["order_quantity"] = order.Quantity
		detail["position_quantity"] = p.Quantity
	case math.Abs(filled-p.EntryPrice) > priceEpsilon:
		detail["problem"] = "price_mismatch"
		detail["filled_price"] = filled
		detail["entry_price"] = p.EntryPrice
	case order.LimitPrice != nil && worseThanLimit(order.Side, filled, *order.LimitPrice):
		detail["problem"] = "filled_beyond_limit"
		detail["filled_price"] = filled
		detail["limit_price"] = *order.LimitPrice
	default:
		return "", nil, nil
	}
	return domain.KillReasonFillDiscrepancy, detail, nil
}

func sideMatches(orderSide, positionSide string) bool {
	return (orderSide == domain.OrderSideBuy && positionSide == domain.PositionSideLong) ||
		(orderSide == domain.OrderSideSell && positionSide == domain.PositionSideShort)
}

// worseThanLimit reports whether a fill at price violates a limit order:
// a BUY must fill at or below its limit, a SELL at or above it.
func worseThanLimit(side string, price, limit float64) bool {
	if side == domain.OrderSideBuy {
		return price > limit+priceEpsilon
	}
	return price < limit-priceEpsilon
}
