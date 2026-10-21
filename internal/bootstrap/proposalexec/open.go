package proposalexec

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/domain/orderproposal"
	"github.com/ousiassllc/pitha-trador/internal/repository/proposalstore"
	"github.com/ousiassllc/pitha-trador/internal/service/execution"
	"github.com/ousiassllc/pitha-trador/internal/service/fillmodel"
	"github.com/ousiassllc/pitha-trador/internal/service/risk"
	"github.com/ousiassllc/pitha-trador/internal/service/risk/bucket"
	"github.com/ousiassllc/pitha-trador/internal/service/risk/sizing"
)

// maxLossTolerance absorbs float rounding when the estimated loss sits exactly
// on the max_trade_loss_pct limit (the Risk sizing is built to land there).
const maxLossTolerance = 1e-9

func bucketMatches(inst domain.Instrument, want string) bool {
	got, err := bucket.Of(inst.AssetClass)
	return err == nil && got == want
}

func instrumentLinks(inst domain.Instrument) proposalstore.Links {
	if inst.ID == 0 {
		return proposalstore.Links{}
	}
	return proposalstore.Links{InstrumentID: &inst.ID}
}

// openLong is steps 4-11 of the flow for a new entry.
func (p Processor) openLong(ctx context.Context, pr orderproposal.Proposal, inst domain.Instrument, now time.Time) error {
	links := proposalstore.Links{InstrumentID: &inst.ID}
	if !p.Execution.EntrySessionOpenNow() {
		// Wait for the next session; the cycle retries until expires_at.
		return p.settle(ctx, pr, orderproposal.StatusWaitingSession, ReasonOutsideSession, links)
	}
	snaps, err := p.Snapshots.ListByInstrument(ctx, inst.ID, 1)
	if err != nil {
		return p.fail(ctx, pr, links, fmt.Errorf("proposalexec: latest snapshot of %q: %w", pr.Symbol, err))
	}
	if len(snaps) == 0 || snaps[0].IsStale(now, p.SnapshotMaxAge) {
		return p.settle(ctx, pr, orderproposal.StatusRejected, ReasonStaleQuote, links)
	}
	snap := snaps[0]

	// The proposal's stop must lie below the price the entry is expected to
	// fill at; one at or above it is no stop for a buy, and is rejected, not
	// swapped for the application's (issue #857). Before Risk and the signal,
	// so nothing is written and nothing is ordered.
	if expected := expectedEntryPrice(pr, snap.Price); pr.StopLossPrice != nil && *pr.StopLossPrice >= expected {
		return p.rejectStopLoss(ctx, pr, links, snap.Price, expected)
	}

	var final proposalstore.Change
	passed, reason, err := p.Risk.CheckThenEnter(ctx, inst.ID, domain.JevDirectionLong, func(size risk.EntrySize) error {
		var enterErr error
		final, enterErr = p.enter(ctx, pr, inst, snap, size, now, links)
		return enterErr
	})
	switch {
	case err != nil:
		return p.fail(ctx, pr, final.Links, err)
	case !passed:
		sig, err := p.recordSignal(ctx, pr, inst, snap, now, false, reason)
		if err != nil {
			return p.fail(ctx, pr, links, err)
		}
		links.TradeSignalID = &sig.ID
		return p.settle(ctx, pr, orderproposal.StatusRejected, reasonCode(reason), links)
	}
	return p.Proposals.Transition(ctx, pr.ID, orderproposal.StatusProcessing, final)
}

// rejectStopLoss records a proposal whose stop_loss_price is not below the
// price the entry is expected to fill at, with the prices the judgement used.
func (p Processor) rejectStopLoss(ctx context.Context, pr orderproposal.Proposal, links proposalstore.Links, latest, expected float64) error {
	detail, err := json.Marshal(struct {
		LatestPrice        float64 `json:"latest_price"`
		ExpectedEntryPrice float64 `json:"expected_entry_price"`
		StopLossPrice      float64 `json:"stop_loss_price"`
	}{latest, expected, *pr.StopLossPrice})
	if err != nil {
		return p.fail(ctx, pr, links, fmt.Errorf("proposalexec: stop-loss rejection detail: %w", err))
	}
	err = p.Proposals.Transition(ctx, pr.ID, pr.Status, proposalstore.Change{
		ToStatus: orderproposal.StatusRejected, Reason: ReasonStopLossNotBelowPrice, DetailJSON: string(detail), Links: links})
	if errors.Is(err, proposalstore.ErrStatusChanged) {
		return nil
	}
	return err
}

// recordSignal is step 6: one trade_signals row for the proposal, under its own
// policy_version and with no Jev decision behind it. A Risk rejection is
// recorded as the Policy Engine records one, so risk_rejections counts it.
func (p Processor) recordSignal(ctx context.Context, pr orderproposal.Proposal, inst domain.Instrument, snap domain.Snapshot, now time.Time, riskPassed bool, riskReason string) (domain.TradeSignal, error) {
	price := snap.Price
	sig := domain.TradeSignal{
		InstrumentID:        inst.ID,
		Symbol:              inst.Symbol,
		Timestamp:           now,
		Direction:           domain.JevDirectionLong,
		EntryPriceReference: &price,
		PolicyVersion:       orderproposal.PolicyVersion,
		RiskPassed:          riskPassed,
	}
	if !riskPassed {
		reason := "risk_engine_rejected: " + riskReason
		sig.RejectReason = &reason
	}
	out, err := p.Signals.Insert(ctx, sig)
	if err != nil {
		return domain.TradeSignal{}, fmt.Errorf("proposalexec: record trade signal for proposal %d: %w", pr.ID, err)
	}
	return out, nil
}

// enter runs under the Risk lock after Risk passed: the signal row, the
// quantity (steps 8), the maximum-loss estimate (9) and the order (10-11). It
// returns the final change for the proposal. An error is unexpected; a
// rejection is a Change with status rejected.
func (p Processor) enter(ctx context.Context, pr orderproposal.Proposal, inst domain.Instrument, snap domain.Snapshot, size risk.EntrySize, now time.Time, links proposalstore.Links) (proposalstore.Change, error) {
	sig, err := p.recordSignal(ctx, pr, inst, snap, now, true, "")
	if err != nil {
		return proposalstore.Change{Links: links}, err
	}
	links.TradeSignalID = &sig.ID
	reject := func(reason string) (proposalstore.Change, error) {
		return proposalstore.Change{ToStatus: orderproposal.StatusRejected, Reason: reason, Links: links}, nil
	}

	expected := expectedEntryPrice(pr, snap.Price)
	requested := requestedQuantity(pr, expected)
	applied := min(requested, size.Quantity)
	if applied <= 0 {
		return reject(ReasonSizeZero)
	}
	stop := effectiveStopLoss(pr, expected, p.Execution.Config().StopLossPct)
	loss := float64(applied) * (expected - stop)
	links.AppliedQuantity, links.ComputedMaxLossJPY, links.EffectiveStopLossPrice = &applied, &loss, &stop

	limits, err := p.Risk.EffectiveLimits(ctx)
	if err != nil {
		return proposalstore.Change{Links: links}, fmt.Errorf("proposalexec: read risk limits: %w", err)
	}
	if loss > limits.InitialCapital*limits.MaxTradeLossPct/100*(1+maxLossTolerance) {
		return reject(ReasonMaxTradeLoss)
	}

	// Record the plan before the order goes out, so a restart can tell what was
	// already decided. A reduction is an event of its own.
	if applied < requested {
		detail := fmt.Sprintf(`{"requested_quantity":%d,"applied_quantity":%d,"limited_by":"position_size"}`, requested, applied)
		err = p.Proposals.Transition(ctx, pr.ID, orderproposal.StatusProcessing,
			proposalstore.Change{Reason: ReasonQuantityReduced, DetailJSON: detail, Links: links})
	} else {
		err = p.Proposals.Link(ctx, pr.ID, links)
	}
	if err != nil {
		return proposalstore.Change{Links: links}, err
	}

	res, err := p.Execution.Enter(ctx, execution.EntryRequest{
		Signal: sig, Quantity: applied, OrderType: pr.OrderType, LimitPrice: pr.LimitPrice,
		Price: snap.Price, Book: fillmodel.BookOf(snap), Now: now,
	})
	if reason, ok := executionRejection(err); ok {
		if reason == ReasonOutsideSession {
			// The session closed between the check and the order: wait for the next one.
			return proposalstore.Change{ToStatus: orderproposal.StatusWaitingSession, Reason: reason, Links: links}, nil
		}
		return reject(reason)
	}
	if err != nil {
		return proposalstore.Change{Links: links}, err
	}
	links.PaperOrderID = &res.Order.ID
	if res.Position == nil {
		return proposalstore.Change{ToStatus: orderproposal.StatusSubmitted, Links: links}, nil
	}
	links.PositionID = &res.Position.ID
	return proposalstore.Change{ToStatus: orderproposal.StatusFilled, Links: links}, nil
}

// executionRejection maps Execution's per-symbol refusals (step 10) to their
// fixed codes. Anything else is not a refusal.
func executionRejection(err error) (string, bool) {
	switch {
	case errors.Is(err, execution.ErrPositionAlreadyOpen):
		return ReasonPositionOpen, true
	case errors.Is(err, execution.ErrPendingOrderExists):
		return ReasonPendingOrder, true
	case errors.Is(err, execution.ErrSymbolInCooldown):
		return ReasonSymbolInCooldown, true
	case errors.Is(err, execution.ErrOutsideTradingSession):
		return ReasonOutsideSession, true
	}
	return "", false
}

// reasonCode is the fixed code at the head of a Risk reason ("cooldown_after_loss:
// retry_after=..." -> "cooldown_after_loss"); detail text is not recorded.
func reasonCode(reason string) string {
	head, _, _ := strings.Cut(reason, ":")
	if head = strings.TrimSpace(head); head != "" {
		return head
	}
	slog.Warn("proposalexec: risk rejection without a reason code", "reason", reason)
	return risk.ReasonRiskEngineError
}

// expectedEntryPrice is the price the entry is expected to fill at: the latest
// price, or for a limit order the limit when that is lower (it then fills at
// the limit or better, never above it).
func expectedEntryPrice(pr orderproposal.Proposal, latest float64) float64 {
	if pr.OrderType == domain.OrderTypeLimit && pr.LimitPrice != nil && *pr.LimitPrice < latest {
		return *pr.LimitPrice
	}
	return latest
}

// requestedQuantity is the proposal's share count: its quantity, or its budget
// divided by the price, rounded down to whole trading units.
func requestedQuantity(pr orderproposal.Proposal, price float64) int64 {
	if pr.Quantity != nil {
		return *pr.Quantity
	}
	if pr.BudgetJPY == nil || price <= 0 {
		return 0
	}
	lots := math.Floor(*pr.BudgetJPY / price / sizing.TradingLotSize)
	return int64(lots) * sizing.TradingLotSize
}

// effectiveStopLoss is the stop the maximum loss is judged against: the
// proposal's or the application's (the slot's Exit stop-loss percentage below
// the entry), whichever is farther below the entry, so the estimate errs on the
// side of the larger loss.
func effectiveStopLoss(pr orderproposal.Proposal, entry, stopLossPct float64) float64 {
	appStop := entry * (1 - stopLossPct/100)
	if pr.StopLossPrice != nil && *pr.StopLossPrice < appStop {
		return *pr.StopLossPrice
	}
	return appStop
}
