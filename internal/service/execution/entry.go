package execution

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// EntryRequest is Enter's input: a Risk-Engine-approved trade signal
// (FR-POLICY-3/5) plus the sizing/order-type decision a caller (a later
// Scheduler paper-execution job, or a test) has already made. Execution
// does not compute Quantity itself - no position-sizing formula is
// defined by functional.md §4.8 (only Risk Engine's exposure limits,
// which Policy Engine already checked before RiskPassed became true).
type EntryRequest struct {
	Signal   domain.TradeSignal
	Quantity int64

	// OrderType is domain.OrderTypeMarket or domain.OrderTypeLimit. Empty
	// resolves to Config.PreferLimit's default (FR-ENTRY-2).
	OrderType string
	// LimitPrice is required when OrderType resolves to
	// domain.OrderTypeLimit.
	LimitPrice *float64
	// Price is the current reference price: a market order fills at
	// Price immediately; a limit order fills immediately only if Price
	// already satisfies LimitPrice (buy at-or-below / sell at-or-above),
	// otherwise the order stays domain.OrderStatusPending until a later
	// TryFillPending call observes a crossing price.
	Price float64
	// Now defaults to Engine's Config.Now() when zero.
	Now time.Time
}

// EntryResult is Enter/TryFillPending's output: the paper_orders row
// always exists; Position is nil when the order stayed PENDING (a limit
// order that has not crossed yet).
type EntryResult struct {
	Order    domain.PaperOrder
	Position *domain.Position
}

// Enter implements Paper Entry (FR-ENTRY-1〜2): submits a market or limit
// paper_orders row for req.Signal's direction/instrument, filling and
// opening a positions row immediately when the order type/price allow it.
// With Config.Calendar set, entries outside 東証立会時間 fail with
// ErrOutsideTradingSession.
func (e *Engine) Enter(ctx context.Context, req EntryRequest) (EntryResult, error) {
	direction := req.Signal.Direction
	if direction != domain.JevDirectionLong && direction != domain.JevDirectionShort {
		return EntryResult{}, ErrDirectionInvalid
	}
	if !req.Signal.RiskPassed {
		return EntryResult{}, ErrRiskNotPassed
	}

	now := req.Now
	if now.IsZero() {
		now = e.cfg.Now()
	}
	if !e.sessionOpen(now) {
		return EntryResult{}, ErrOutsideTradingSession
	}

	if _, err := e.positions.GetOpenByInstrument(ctx, req.Signal.InstrumentID); err == nil {
		return EntryResult{}, ErrPositionAlreadyOpen
	} else if !errors.Is(err, domain.ErrPositionNotFound) {
		return EntryResult{}, fmt.Errorf("execution: check open position for instrument %d: %w", req.Signal.InstrumentID, err)
	}

	if until, inCooldown := e.symbolCooldown(req.Signal.Symbol, now); inCooldown {
		return EntryResult{}, fmt.Errorf("%w: retry_after=%s", ErrSymbolInCooldown, until.Format(time.RFC3339))
	}

	orderType := req.OrderType
	if orderType == "" {
		if e.cfg.PreferLimit {
			orderType = domain.OrderTypeLimit
		} else {
			orderType = domain.OrderTypeMarket
		}
	}
	if orderType == domain.OrderTypeLimit && req.LimitPrice == nil {
		return EntryResult{}, ErrLimitPriceRequired
	}

	side := domain.OrderSideBuy
	if direction == domain.JevDirectionShort {
		side = domain.OrderSideSell
	}

	order, err := e.orders.Insert(ctx, domain.PaperOrder{
		InstrumentID:  req.Signal.InstrumentID,
		TradeSignalID: signalIDPtr(req.Signal),
		Symbol:        req.Signal.Symbol,
		Side:          side,
		OrderType:     orderType,
		Quantity:      req.Quantity,
		LimitPrice:    req.LimitPrice,
		Status:        domain.OrderStatusPending,
		SubmittedAt:   now,
	})
	if err != nil {
		return EntryResult{}, fmt.Errorf("execution: submit entry order for %q: %w", req.Signal.Symbol, err)
	}

	if orderType == domain.OrderTypeMarket || limitCrosses(side, *req.LimitPrice, req.Price) {
		result, err := e.fillEntry(ctx, order, direction, req.Price, now)
		if err != nil {
			// fillEntry rolled back, so the order is still PENDING and
			// nothing retries a market order: reject it instead of
			// leaving it dangling (a concurrent Enter for the same
			// instrument lands here via positions_open_instrument_uq).
			if _, rejectErr := e.orders.UpdateStatus(ctx, order.ID, domain.OrderStatusRejected); rejectErr != nil {
				err = errors.Join(err, fmt.Errorf("execution: reject unfilled entry order %d: %w", order.ID, rejectErr))
			}
			return EntryResult{}, err
		}
		return result, nil
	}
	return EntryResult{Order: order}, nil
}

// TryFillPending attempts to fill a still-PENDING limit entry order at
// currentPrice, opening its position if the price now crosses the
// order's limit (FR-ENTRY-1's limit-order path continuing past Enter's
// initial check). It returns ok=false without error if orderID is not a
// PENDING limit order or currentPrice has not crossed yet.
func (e *Engine) TryFillPending(ctx context.Context, orderID int64, direction string, currentPrice float64, now time.Time) (EntryResult, bool, error) {
	if !validPrice(currentPrice) {
		return EntryResult{}, false, fmt.Errorf("execution: fill pending order %d: %w (got %v)", orderID, ErrInvalidPrice, currentPrice)
	}
	order, err := e.orders.Get(ctx, orderID)
	if err != nil {
		return EntryResult{}, false, fmt.Errorf("execution: get pending order %d: %w", orderID, err)
	}
	if order.Status != domain.OrderStatusPending || order.OrderType != domain.OrderTypeLimit || order.LimitPrice == nil {
		return EntryResult{}, false, nil
	}
	if !limitCrosses(order.Side, *order.LimitPrice, currentPrice) {
		return EntryResult{}, false, nil
	}

	result, err := e.fillEntry(ctx, order, direction, currentPrice, now)
	if err != nil {
		return EntryResult{}, false, err
	}
	return result, true, nil
}

// fillEntry fills order at price/now and opens the resulting position in a
// single transaction (OrderRepository.FillEntry): a failure opening the
// position leaves the order un-filled rather than FILLED with no position
// (issue #158).
func (e *Engine) fillEntry(ctx context.Context, order domain.PaperOrder, direction string, price float64, now time.Time) (EntryResult, error) {
	positionSide := domain.PositionSideLong
	if direction == domain.JevDirectionShort {
		positionSide = domain.PositionSideShort
	}

	filled, position, err := e.orders.FillEntry(ctx, order.ID, price, nil, now, domain.Position{
		InstrumentID: order.InstrumentID,
		Symbol:       order.Symbol,
		Side:         positionSide,
		Quantity:     order.Quantity,
		EntryPrice:   price,
		CurrentPrice: price,
		OpenedAt:     now,
	})
	if err != nil {
		return EntryResult{}, fmt.Errorf("execution: fill entry order %d and open position for %q: %w", order.ID, order.Symbol, err)
	}
	return EntryResult{Order: filled, Position: &position}, nil
}

// limitCrosses reports whether price already satisfies a limit order on
// side: a BUY limit fills at or below its limit price; a SELL limit fills
// at or above it.
func limitCrosses(side string, limitPrice, price float64) bool {
	if side == domain.OrderSideBuy {
		return price <= limitPrice
	}
	return price >= limitPrice
}

func signalIDPtr(s domain.TradeSignal) *int64 {
	if s.ID == 0 {
		return nil
	}
	id := s.ID
	return &id
}
