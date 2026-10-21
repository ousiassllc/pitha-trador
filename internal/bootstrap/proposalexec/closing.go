package proposalexec

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/domain/orderproposal"
	"github.com/ousiassllc/pitha-trador/internal/repository/proposalstore"
	"github.com/ousiassllc/pitha-trador/internal/service/execution"
)

// closePosition is an action=close proposal: the symbol's open position is
// closed at market through Execution's existing exit path
// (CloseAtMarket -> Close), with exit_reason proposal_close.
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
