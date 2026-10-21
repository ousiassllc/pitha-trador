package proposalexec_test

import (
	"context"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap/proposalexec"
	"github.com/ousiassllc/pitha-trador/internal/config/dailyguardsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/domain/orderproposal"
)

func closeSpec() spec { return spec{action: orderproposal.ActionClose} }

func TestClose_ClosesTheHeldPositionWithProposalCloseReason(t *testing.T) {
	f := newFixture(t)
	pos := f.openPosition(100, 2500)
	id := f.propose(closeSpec())
	f.cycle()
	p := f.wantEnd(id, orderproposal.StatusClosed, "")
	closed, err := f.svc.Positions.Get(context.Background(), pos.ID)
	if err != nil || closed.IsOpen() || closed.ExitReason == nil || *closed.ExitReason != "proposal_close" {
		t.Fatalf("position = (%+v, %v), want closed with exit_reason proposal_close", closed, err)
	}
	if p.PositionID == nil || *p.PositionID != pos.ID || p.PaperOrderID == nil || closed.ExitOrderID == nil || *p.PaperOrderID != *closed.ExitOrderID {
		t.Fatalf("links = position %v order %v, want position %d and its exit order", p.PositionID, p.PaperOrderID, pos.ID)
	}
	if f.exec.enters != 0 || f.count("trade_signals", "") != 0 {
		t.Fatal("a close went through Enter or wrote a signal")
	}
}

func TestClose_WithoutAnOpenPositionIsRejected(t *testing.T) {
	f := newFixture(t)
	id := f.propose(closeSpec())
	f.cycle()
	f.wantEnd(id, orderproposal.StatusRejected, proposalexec.ReasonNoOpenPosition)
}

// Closing only reduces risk: the Kill Switch, a pause and the daily guard do
// not stop it, while the open_long of the same state is rejected.
func TestClose_RunsWhileKillSwitchPauseAndDailyGuardBlockNewEntries(t *testing.T) {
	for name, block := range map[string]func(t *testing.T, f *fixture){
		"kill switch": func(t *testing.T, f *fixture) {
			if err := f.svc.Risk.Kill(context.Background()); err != nil {
				t.Fatal(err)
			}
		},
		"daily guard": func(t *testing.T, f *fixture) {
			f.haltDailyGuard(dailyguardsettings.KeyMaxTrades, "3", domain.DailyHaltReasonMaxTrades)
		},
		"paused": func(t *testing.T, f *fixture) {
			if err := f.svc.Risk.Pause(context.Background()); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			block(t, f)
			// The position is opened after the block: a Kill Switch force-closes what is held when it fires.
			pos := f.openPosition(100, 2500)
			open := f.propose(spec{})
			closing := f.propose(closeSpec())
			f.cycle()
			f.wantEnd(closing, orderproposal.StatusClosed, "")
			if p, _ := f.svc.Positions.Get(context.Background(), pos.ID); p.IsOpen() {
				t.Fatal("the position is still open")
			}
			// The open_long proposed in the same state is rejected by Risk (it needs no position).
			if p := f.get(open); p.Status != orderproposal.StatusRejected || p.TradeSignalID == nil {
				t.Fatalf("open_long = (%s, %q), want rejected by Risk", p.Status, p.StatusReason)
			}
		})
	}
}

func TestClose_OutsideTheSessionWaitsAndClosesWhenItOpens(t *testing.T) {
	f := newFixture(t)
	pos := f.openPosition(100, 2500)
	*f.clock = at(8, 30, 0)
	id := f.propose(closeSpec())
	f.cycle()
	f.wantEnd(id, orderproposal.StatusWaitingSession, proposalexec.ReasonOutsideSession)
	if p, _ := f.svc.Positions.Get(context.Background(), pos.ID); !p.IsOpen() {
		t.Fatal("the position was closed outside the session")
	}
	*f.clock = at(10, 0, 30)
	f.cycle()
	f.wantEnd(id, orderproposal.StatusClosed, "")
}
