package proposalexec

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/domain/orderproposal"
	"github.com/ousiassllc/pitha-trador/internal/repository/proposalstore"
	"github.com/ousiassllc/pitha-trador/internal/service/execution"
)

// syncSubmitted follows a proposal whose limit order is waiting: it expires or
// is cancelled (the order goes CANCELLED) when its time is up or a cancellation
// was requested, and otherwise mirrors the order's outcome. Execution's own
// per-bar step fills the order; this only reads what happened.
func (p Processor) syncSubmitted(ctx context.Context, pr orderproposal.Proposal, now time.Time) error {
	if pr.PaperOrderID == nil {
		return p.fail(ctx, pr, proposalstore.Links{}, fmt.Errorf("proposalexec: submitted proposal %d has no order", pr.ID))
	}
	reason, to := "", ""
	switch {
	case !now.Before(pr.ExpiresAt):
		reason, to = ReasonExpired, orderproposal.StatusExpired
	case pr.CancelRequested():
		reason, to = ReasonCancelRequested, orderproposal.StatusCancelled
	}
	if to != "" {
		err := p.Execution.CancelPendingEntry(ctx, *pr.PaperOrderID)
		switch {
		case err == nil:
			return p.settle(ctx, pr, to, reason, proposalstore.Links{})
		case !errors.Is(err, execution.ErrOrderNotPending):
			return err
		}
		// The order is no longer pending (it filled meanwhile): what it became wins.
	}
	return p.mirrorOrder(ctx, pr, "")
}

// mirrorOrder settles pr to the state of its paper order: FILLED -> filled
// (with the position), CANCELLED/REJECTED -> cancelled/rejected, PENDING ->
// submitted. detail is recorded on the event (e.g. a restart recovery).
func (p Processor) mirrorOrder(ctx context.Context, pr orderproposal.Proposal, detail string) error {
	order, err := p.Orders.Get(ctx, *pr.PaperOrderID)
	if err != nil {
		return err
	}
	change := proposalstore.Change{DetailJSON: detail}
	switch order.Status {
	case domain.OrderStatusPending:
		if pr.Status == orderproposal.StatusSubmitted {
			return nil
		}
		change.ToStatus = orderproposal.StatusSubmitted
		change.Links.PaperOrderID = &order.ID
	case domain.OrderStatusFilled:
		pos, err := p.positionOfEntry(ctx, order)
		if err != nil {
			return fmt.Errorf("proposalexec: position of filled order %d: %w", order.ID, err)
		}
		change.ToStatus = orderproposal.StatusFilled
		change.Links.PaperOrderID, change.Links.PositionID = &order.ID, &pos.ID
	case domain.OrderStatusCancelled:
		change.ToStatus, change.Reason = orderproposal.StatusCancelled, ReasonOrderCancelled
	default:
		change.ToStatus, change.Reason = orderproposal.StatusRejected, ReasonOrderRejected
	}
	err = p.Proposals.Transition(ctx, pr.ID, pr.Status, change)
	if errors.Is(err, proposalstore.ErrStatusChanged) {
		return nil
	}
	return err
}

// Recover handles the proposals a previous run left in processing (the app
// stopped mid-flight), once at startup before Run. Whether an order went out is
// read from the database, never guessed: a proposal whose signal already became
// an order (or whose position was already closed) takes that outcome, one past
// its expires_at with no order becomes expired, and any other goes back to
// received to be processed again. The same proposal never gets a second order.
// One proposal's failure does not stop the others: it is logged and that
// proposal becomes failed (processing_error), so none stays in processing. It
// returns how many proposals it looked at and the failures joined.
func (p Processor) Recover(ctx context.Context) (int, error) {
	if err := p.paperOnly(); err != nil {
		return 0, err
	}
	stuck, err := p.Proposals.Processing(ctx)
	if err != nil {
		return 0, err
	}
	var errs []error
	for _, pr := range stuck {
		if err := p.recoverOne(ctx, pr); err != nil {
			errs = append(errs, fmt.Errorf("proposalexec: recover proposal %d: %w", pr.ID, err))
			// fail logs the cause; a failure to record it is the only way one stays in processing.
			if err := p.fail(ctx, pr, proposalstore.Links{}, err); err != nil {
				errs = append(errs, fmt.Errorf("proposalexec: proposal %d left in processing: %w", pr.ID, err))
			}
		}
	}
	p.logRecovered(ctx, stuck)
	return len(stuck), errors.Join(errs...)
}

// logRecovered logs how the proposals Recover looked at ended up (by status).
func (p Processor) logRecovered(ctx context.Context, stuck []orderproposal.Proposal) {
	if len(stuck) == 0 {
		return
	}
	byStatus := map[string]int{}
	for _, pr := range stuck {
		if now, err := p.Proposals.Get(ctx, pr.ID); err == nil {
			byStatus[now.Status]++
		} else {
			byStatus["unknown"]++
		}
	}
	slog.InfoContext(ctx, "proposalexec: recovery settled proposals left in processing", "count", len(stuck),
		"expired", byStatus[orderproposal.StatusExpired], "received", byStatus[orderproposal.StatusReceived],
		"failed", byStatus[orderproposal.StatusFailed], "still_processing", byStatus[orderproposal.StatusProcessing],
		"by_status", byStatus)
}

const recoveredDetail = `{"recovered_after_restart":true}`

func (p Processor) recoverOne(ctx context.Context, pr orderproposal.Proposal) error {
	if pr.Action == orderproposal.ActionClose {
		return p.recoverClose(ctx, pr)
	}
	if pr.TradeSignalID != nil && pr.InstrumentID != nil {
		orders, err := p.Orders.ListByInstrument(ctx, *pr.InstrumentID, orderScanLimit)
		if err != nil {
			return err
		}
		for _, o := range orders { // newest first: the order the proposal's signal became
			if o.TradeSignalID != nil && *o.TradeSignalID == *pr.TradeSignalID {
				pr.PaperOrderID = &o.ID
				return p.mirrorOrder(ctx, pr, recoveredDetail)
			}
		}
	}
	return p.requeue(ctx, pr)
}

// orderScanLimit is how many of an instrument's latest orders recovery looks
// through for the proposal's own (an entry per symbol is one order at a time).
const orderScanLimit = 20

// positionOfEntry is the position order opened: the symbol's open one when it
// came from this order, else one closed since the order was submitted.
func (p Processor) positionOfEntry(ctx context.Context, order domain.PaperOrder) (domain.Position, error) {
	open, err := p.Positions.GetOpenByInstrument(ctx, order.InstrumentID)
	if err == nil && open.EntryOrderID == order.ID {
		return open, nil
	}
	if err != nil && !errors.Is(err, domain.ErrPositionNotFound) {
		return domain.Position{}, err
	}
	closed, err := p.Positions.ListClosedBetween(ctx, order.SubmittedAt, p.now().Add(time.Minute))
	if err != nil {
		return domain.Position{}, err
	}
	for _, pos := range closed {
		if pos.EntryOrderID == order.ID {
			return pos, nil
		}
	}
	return domain.Position{}, domain.ErrPositionNotFound
}

// recoverClose finds out whether the close already happened: the symbol has no
// open position and one closed with proposal_close since the proposal arrived.
func (p Processor) recoverClose(ctx context.Context, pr orderproposal.Proposal) error {
	if pr.InstrumentID != nil {
		if _, err := p.Positions.GetOpenByInstrument(ctx, *pr.InstrumentID); err == nil {
			return p.requeue(ctx, pr) // still open: close it (again) from the start
		} else if !errors.Is(err, domain.ErrPositionNotFound) {
			return err
		}
		closed, err := p.Positions.ListClosedBetween(ctx, pr.ReceivedAt, p.now().Add(time.Minute))
		if err != nil {
			return err
		}
		for _, pos := range closed {
			if pos.InstrumentID == *pr.InstrumentID && pos.ExitReason != nil && *pos.ExitReason == domain.ExitReasonProposalClose {
				links := proposalstore.Links{PositionID: &pos.ID, PaperOrderID: pos.ExitOrderID}
				return p.Proposals.Transition(ctx, pr.ID, pr.Status, proposalstore.Change{
					ToStatus: orderproposal.StatusClosed, DetailJSON: recoveredDetail, Links: links})
			}
		}
	}
	return p.requeue(ctx, pr)
}

// requeue settles a processing proposal that has no outcome in the database: a
// proposal past its expires_at (and with no order) is expired right away, any
// other goes back to received to be processed again.
func (p Processor) requeue(ctx context.Context, pr orderproposal.Proposal) error {
	change := proposalstore.Change{ToStatus: orderproposal.StatusReceived, Reason: ReasonRecoveredAfterStop, DetailJSON: recoveredDetail}
	if !p.now().Before(pr.ExpiresAt) {
		change.ToStatus, change.Reason = orderproposal.StatusExpired, ReasonExpired
	}
	return p.Proposals.Transition(ctx, pr.ID, pr.Status, change)
}
