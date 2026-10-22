package proposalexec

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/domain/orderproposal"
	"github.com/ousiassllc/pitha-trador/internal/repository/proposalstore"
	"github.com/ousiassllc/pitha-trador/internal/repository/trading"
	"github.com/ousiassllc/pitha-trador/internal/service/execution"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/origin"
)

// closePosition is an action=close proposal: the symbol's open position is
// closed at market through Execution's existing exit path
// (CloseAtMarket -> Close), with exit_reason proposal_close. Only a position an
// order proposal opened is closed (issue #859): one the Policy Engine or the
// bootstrap opened is rejected as position_not_from_proposal, untouched.
//
// No Risk check runs: a close only reduces risk, so the Kill Switch, a pause
// and the daily guard do not stop it (the same as the operator's manual close).
func (p Processor) closePosition(ctx context.Context, pr orderproposal.Proposal, inst domain.Instrument, now time.Time) error {
	links := proposalstore.Links{InstrumentID: &inst.ID}
	pos, err := p.Positions.GetOpenByInstrument(ctx, inst.ID)
	if errors.Is(err, domain.ErrPositionNotFound) {
		return p.settle(ctx, pr, orderproposal.StatusRejected, ReasonNoOpenPosition, links)
	}
	if err != nil {
		return p.fail(ctx, pr, links, fmt.Errorf("proposalexec: open position of %q: %w", pr.Symbol, err))
	}
	fromProposal, err := p.enteredByProposal(ctx, pos)
	if err != nil {
		return p.fail(ctx, pr, links, fmt.Errorf("proposalexec: entry of position %d: %w", pos.ID, err))
	}
	if !fromProposal {
		return p.settle(ctx, pr, orderproposal.StatusRejected, ReasonPositionNotFromProposal, links)
	}
	if !p.tradable(pr.Action, now) {
		return p.settle(ctx, pr, orderproposal.StatusWaitingSession, ReasonOutsideSession, links)
	}
	links.PositionID, links.AppliedQuantity = &pos.ID, &pos.Quantity
	closed, err := p.Execution.CloseAtMarket(ctx, pos.ID, domain.ExitReasonProposalClose, now)
	switch {
	case errors.Is(err, domain.ErrPositionAlreadyClosed):
		return p.settle(ctx, pr, orderproposal.StatusRejected, ReasonNoOpenPosition, instrumentLinks(inst))
	case errors.Is(err, execution.ErrOutsideTradingSession):
		// The market closed between the check and the fill.
		return p.settle(ctx, pr, orderproposal.StatusWaitingSession, ReasonOutsideSession, links)
	case err != nil:
		return p.fail(ctx, pr, links, err)
	}
	links.PaperOrderID = closed.ExitOrderID
	return p.settle(ctx, pr, orderproposal.StatusClosed, "", links)
}

// enteredByProposal reports whether an order proposal opened pos. It is read
// from the database, never guessed: the signal behind the position's entry
// order is an order proposal's by the definition of the origin that
// paper_trades and paper_performance_summary report (origin.Of ==
// origin.OrderProposal), or an open_long proposal is linked to the position.
// A position with neither (the Policy Engine's, the bootstrap's, a manual
// order without a signal) is not one a proposal may close.
func (p Processor) enteredByProposal(ctx context.Context, pos domain.Position) (bool, error) {
	order, err := p.Orders.Get(ctx, pos.EntryOrderID)
	switch {
	case err == nil && order.TradeSignalID != nil:
		sig, err := p.Signals.Get(ctx, *order.TradeSignalID)
		if err != nil {
			return false, err
		}
		if origin.Of(sql.NullString{String: sig.PolicyVersion, Valid: true}) == origin.OrderProposal {
			return true, nil
		}
	case err != nil && !errors.Is(err, trading.ErrOrderNotFound):
		return false, err
	}
	return p.Proposals.OpenedPosition(ctx, pos.ID)
}
