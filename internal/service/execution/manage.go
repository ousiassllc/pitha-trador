package execution

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/execution/enrich"
	"github.com/ousiassllc/pitha-trador/internal/service/execution/vwapcross"
)

// pendingOrderScanLimit bounds how many of an instrument's most recent
// paper_orders rows OnSnapshot scans for PENDING limit entries. Enter
// allows at most one open position per instrument, so only a handful of
// recent orders can ever still be pending.
const pendingOrderScanLimit = 20

// latestTraderDecisionScanLimit is how many recent jev_decisions rows
// OnSnapshot scans for the latest Jev Trader decision (Scout/Trader rows
// interleave per scan cycle; mirrors State's own reasoning).
const latestTraderDecisionScanLimit = 50

// SnapshotResult is OnSnapshot's output: every entry filled and the
// instrument's open position after this market update (nil when none), and
// Exited reports whether that position was closed by an FR-EXIT-1
// condition during this update.
type SnapshotResult struct {
	Filled   []EntryResult
	Position *domain.Position
	Exited   bool
}

// OnSnapshot is Paper Trading's per-market-update step for snap's
// instrument (functional.md §4.8, §4.10 FR-SCHED-4): it fills any PENDING
// limit entry order snap.Price now crosses, marks the open position to
// market, then evaluates FR-EXIT-1's exit conditions against snap and the
// latest Jev Trader decision, closing the position at snap.Price when one
// triggers. It requires Deps.Decisions to have been set on NewEngine.
func (e *Engine) OnSnapshot(ctx context.Context, snap domain.Snapshot) (SnapshotResult, error) {
	if e.decisions == nil {
		return SnapshotResult{}, fmt.Errorf("execution: OnSnapshot requires Deps.Decisions to be configured")
	}
	if !validPrice(snap.Price) {
		return SnapshotResult{}, fmt.Errorf("execution: snapshot for %q: %w (got %v)", snap.Symbol, ErrInvalidPrice, snap.Price)
	}
	e.snapshotMu.Lock()
	defer e.snapshotMu.Unlock()
	now := snap.Timestamp

	result := SnapshotResult{Filled: e.fillPendingEntries(ctx, snap)}

	position, err := e.positions.GetOpenByInstrument(ctx, snap.InstrumentID)
	if errors.Is(err, domain.ErrPositionNotFound) {
		return result, nil
	}
	if err != nil {
		return SnapshotResult{}, fmt.Errorf("execution: open position for %q: %w", snap.Symbol, err)
	}

	unrealized := positionSign(position.Side) * float64(position.Quantity) * (snap.Price - position.EntryPrice)
	position, err = e.positions.Mark(ctx, position.ID, snap.Price, unrealized, now)
	if err != nil {
		return SnapshotResult{}, fmt.Errorf("execution: mark position %d to market: %w", position.ID, err)
	}

	decision, err := e.latestTraderDecision(ctx, snap.InstrumentID)
	if err != nil {
		return SnapshotResult{}, err
	}
	mkt := MarketContext{Price: snap.Price, Decision: decision, MarketCloseAt: e.marketCloseAt(now), Now: now}
	if snap.Feature.VWAP > 0 {
		vwap := snap.Feature.VWAP
		mkt.VWAP = &vwap
		if prev, ok := e.vwapObs.Previous(snap.InstrumentID, position.ID); ok {
			mkt.PrevVWAP = &prev
		}
	}

	reason, exit, err := e.EvaluateExit(ctx, position, mkt)
	if err != nil {
		return SnapshotResult{}, fmt.Errorf("execution: evaluate exit for position %d: %w", position.ID, err)
	}
	if mkt.VWAP != nil {
		e.vwapObs.Record(snap.InstrumentID, vwapcross.Observation{PositionID: position.ID, Price: snap.Price, VWAP: *mkt.VWAP})
	}
	if !exit {
		result.Position = &position
		return result, nil
	}

	closed, err := e.Close(ctx, position.ID, reason, snap.Price, now)
	if errors.Is(err, domain.ErrPositionAlreadyClosed) {
		return result, nil // a concurrent manual close / CloseAll won
	}
	if err != nil {
		return SnapshotResult{}, err
	}
	result.Position = &closed
	result.Exited = true
	return result, nil
}

// fillPendingEntries attempts TryFillPending on every PENDING limit entry
// order of snap's instrument. It never fails: a problem with one pending
// order must not stop OnSnapshot from marking and evaluating exits for the
// open position (issue #343), so failures are logged and the order is
// skipped. A fill that fails because the instrument already has an open
// position (a second PENDING order that lost the race to
// positions_open_instrument_uq) can never succeed while that position is
// open, so that order is rejected instead of being retried forever.
func (e *Engine) fillPendingEntries(ctx context.Context, snap domain.Snapshot) []EntryResult {
	orders, err := e.orders.ListByInstrument(ctx, snap.InstrumentID, pendingOrderScanLimit)
	if err != nil {
		slog.ErrorContext(ctx, "execution: list orders for pending fill", "symbol", snap.Symbol, "error", err)
		return nil
	}

	var filled []EntryResult
	for _, order := range orders {
		if order.Status != domain.OrderStatusPending || order.OrderType != domain.OrderTypeLimit {
			continue
		}
		direction := domain.JevDirectionLong
		if order.Side == domain.OrderSideSell {
			direction = domain.JevDirectionShort
		}
		entry, ok, err := e.TryFillPending(ctx, order.ID, direction, snap.Price, snap.Timestamp)
		if err != nil {
			slog.ErrorContext(ctx, "execution: fill pending entry order", "symbol", snap.Symbol, "order_id", order.ID, "error", err)
			e.rejectIfPositionOpen(ctx, order)
			continue
		}
		if ok {
			filled = append(filled, entry)
		}
	}
	return filled
}

// rejectIfPositionOpen marks the PENDING entry order REJECTED when its
// instrument already has an open position, since it cannot fill until that
// position closes and Enter never queues entries behind an open position.
func (e *Engine) rejectIfPositionOpen(ctx context.Context, order domain.PaperOrder) {
	if _, err := e.positions.GetOpenByInstrument(ctx, order.InstrumentID); err != nil {
		return
	}
	if _, err := e.orders.UpdateStatus(ctx, order.ID, domain.OrderStatusRejected); err != nil {
		slog.ErrorContext(ctx, "execution: reject pending entry order", "symbol", order.Symbol, "order_id", order.ID, "error", err)
	}
}

// latestTraderDecision returns instrumentID's most recent Jev Trader
// decision (enrich.Decision-populated, as EvaluateExit requires), or nil
// when none exists - which disables only the two Jev-derived exit
// conditions (FR-EXIT-3).
func (e *Engine) latestTraderDecision(ctx context.Context, instrumentID int64) (*domain.JevDecision, error) {
	decisions, err := e.decisions.ListByInstrument(ctx, instrumentID, latestTraderDecisionScanLimit)
	if err != nil {
		return nil, fmt.Errorf("execution: recent decisions for instrument %d: %w", instrumentID, err)
	}
	for _, d := range decisions {
		if d.DecisionType == domain.JevDecisionTypeTrader {
			enriched := enrich.Decision(d)
			return &enriched, nil
		}
	}
	return nil, nil
}
