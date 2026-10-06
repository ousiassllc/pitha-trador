package execution

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/fillmodel"
)

// EntryRequest is Enter's input: a Risk-Engine-approved trade signal
// (FR-POLICY-3/5) plus the sizing/order-type decision a caller
// (internal/bootstrap/paperexec, or a test) has already made. Execution
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
	// Price is the current reference (last) price: a market order fills
	// at the quote Book gives around it (Config.Fill: spread, slippage,
	// tick grid, 寄り/引け); a limit order fills immediately only if that
	// touch already satisfies LimitPrice (buy at-or-below / sell at-or-
	// above), otherwise the order stays domain.OrderStatusPending until a
	// later TryFillPending call observes a crossing price.
	Price float64
	// Book is Price's bid/ask quote; the zero value (unknown) fills at
	// Price without crossing a spread.
	Book fillmodel.Book
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
// Quantity <= 0 or a non-finite/non-positive Price or LimitPrice fails with
// ErrInvalidQuantity/ErrInvalidPrice before anything is written (issue
// #342). With Config.Calendar set, entries outside 東証立会時間 fail with
// ErrOutsideTradingSession; with a PENDING entry order already on the
// instrument they fail with ErrPendingOrderExists (issue #343).
func (e *Engine) Enter(ctx context.Context, req EntryRequest) (EntryResult, error) {
	direction := req.Signal.Direction
	if direction != domain.JevDirectionLong && direction != domain.JevDirectionShort {
		return EntryResult{}, ErrDirectionInvalid
	}
	if !req.Signal.RiskPassed {
		return EntryResult{}, ErrRiskNotPassed
	}
	if req.Quantity <= 0 {
		return EntryResult{}, fmt.Errorf("%w (got %d)", ErrInvalidQuantity, req.Quantity)
	}
	if !validPrice(req.Price) || (req.LimitPrice != nil && !validPrice(*req.LimitPrice)) {
		return EntryResult{}, fmt.Errorf("execution: enter %q: %w (price %v)", req.Signal.Symbol, ErrInvalidPrice, req.Price)
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

	pending, err := e.orders.ListByInstrument(ctx, req.Signal.InstrumentID, pendingOrderScanLimit)
	if err != nil {
		return EntryResult{}, fmt.Errorf("execution: check pending orders for instrument %d: %w", req.Signal.InstrumentID, err)
	}
	for _, o := range e.rejectStaleMarketOrders(ctx, pending, now) {
		if o.Status == domain.OrderStatusPending {
			return EntryResult{}, ErrPendingOrderExists
		}
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

	f, fills, err := e.fillFor(orderType, side, req.Quantity, req.LimitPrice, req.Price, req.Book, now)
	if err != nil {
		return EntryResult{}, err
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

	if fills {
		result, err := e.fillEntry(ctx, order, direction, f, now)
		if err != nil {
			// fillEntry rolled back, so the order is still PENDING and
			// nothing retries a market order: reject it instead of
			// leaving it dangling (a concurrent Enter for the same
			// instrument lands here via positions_open_instrument_uq).
			if rejectErr := e.rejectOrder(ctx, order); rejectErr != nil {
				err = errors.Join(err, fmt.Errorf("execution: reject unfilled entry order %d: %w", order.ID, rejectErr))
			}
			return EntryResult{}, err
		}
		return result, nil
	}
	return EntryResult{Order: order}, nil
}

// TryFillPending attempts to fill a still-PENDING limit entry order at
// currentPrice (with its quote book), opening its position if the touch now
// crosses the order's limit (FR-ENTRY-1's limit-order path continuing past
// Enter's initial check). It returns ok=false without error if orderID is
// not a PENDING limit order, the limit has not been crossed yet, or the
// market is closed (昼休み・立会時間外: the order stays PENDING).
func (e *Engine) TryFillPending(ctx context.Context, orderID int64, direction string, currentPrice float64, book fillmodel.Book, now time.Time) (EntryResult, bool, error) {
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
	f, fills, err := e.fillFor(order.OrderType, order.Side, order.Quantity, order.LimitPrice, currentPrice, book, now)
	if errors.Is(err, ErrOutsideTradingSession) || (err == nil && !fills) {
		return EntryResult{}, false, nil
	}
	if err != nil {
		return EntryResult{}, false, err
	}

	result, err := e.fillEntry(ctx, order, direction, f, now)
	if err != nil {
		return EntryResult{}, false, err
	}
	return result, true, nil
}

// fillEntry fills order at f/now and opens the resulting position in a
// single transaction (OrderRepository.FillEntry): a failure opening the
// position leaves the order un-filled rather than FILLED with no position
// (issue #158).
func (e *Engine) fillEntry(ctx context.Context, order domain.PaperOrder, direction string, f fill, now time.Time) (EntryResult, error) {
	positionSide := domain.PositionSideLong
	if direction == domain.JevDirectionShort {
		positionSide = domain.PositionSideShort
	}

	filled, position, err := e.orders.FillEntry(ctx, order.ID, f.price, f.fee, &f.slippageBps, now, domain.Position{
		InstrumentID: order.InstrumentID,
		Symbol:       order.Symbol,
		Side:         positionSide,
		Quantity:     order.Quantity,
		EntryPrice:   f.price,
		CurrentPrice: f.price,
		OpenedAt:     now,
	})
	if err != nil {
		return EntryResult{}, fmt.Errorf("execution: fill entry order %d and open position for %q: %w", order.ID, order.Symbol, err)
	}
	return EntryResult{Order: filled, Position: &position}, nil
}

func signalIDPtr(s domain.TradeSignal) *int64 {
	if s.ID == 0 {
		return nil
	}
	id := s.ID
	return &id
}
