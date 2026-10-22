package proposalexectests

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

// positionStaysOpen fails unless the position is still open and no exit order
// was placed: a rejected close left everything as it was.
func positionStaysOpen(t *testing.T, f *fixture, pos domain.Position) {
	t.Helper()
	p, err := f.svc.Positions.Get(context.Background(), pos.ID)
	if err != nil || !p.IsOpen() || p.ExitReason != nil {
		t.Fatalf("position = (%+v, %v), want still open", p, err)
	}
	if n := f.count("paper_orders", "WHERE side = 'SELL'"); n != 0 || f.exec.closes != 0 {
		t.Fatalf("exit orders = %d, CloseAtMarket calls = %d; want 0 and 0", n, f.exec.closes)
	}
}

// A close proposal only closes a position an order proposal opened (issue
// #859): the Policy Engine's, the bootstrap's and one with no entry signal at
// all are rejected as position_not_from_proposal, untouched, even though the
// symbol is held.
func TestClose_PositionOpenedByThePolicyEngineIsRejected(t *testing.T) {
	for _, version := range []string{"policy-v2", "policy-v2+prop-7"} {
		t.Run(version, func(t *testing.T) {
			f := newFixture(t)
			pos := f.openPositionFrom(version, 100, 2500)
			id := f.propose(closeSpec())
			f.cycle()
			p := f.wantEnd(id, orderproposal.StatusRejected, proposalexec.ReasonPositionNotFromProposal)
			if p.PositionID != nil || p.PaperOrderID != nil {
				t.Fatalf("links = position %v order %v, want none", p.PositionID, p.PaperOrderID)
			}
			positionStaysOpen(t, f, pos)
		})
	}
}

func TestClose_PositionOpenedByBootstrapIsRejected(t *testing.T) {
	for _, version := range []string{domain.PolicyVersionPaperBootstrap, domain.PolicyVersionPaperBootstrap + "+prop-7"} {
		t.Run(version, func(t *testing.T) {
			f := newFixture(t)
			pos := f.openPositionFrom(version, 100, 2500)
			id := f.propose(closeSpec())
			f.cycle()
			f.wantEnd(id, orderproposal.StatusRejected, proposalexec.ReasonPositionNotFromProposal)
			positionStaysOpen(t, f, pos)
		})
	}
}

func TestClose_PositionWithoutAnEntrySignalIsRejected(t *testing.T) {
	f := newFixture(t)
	pos := f.openPositionFrom("", 100, 2500)
	id := f.propose(closeSpec())
	f.cycle()
	f.wantEnd(id, orderproposal.StatusRejected, proposalexec.ReasonPositionNotFromProposal)
	positionStaysOpen(t, f, pos)
}

// Both halves of the definition close: the entry signal of an order proposal
// (the origin paper_trades reports), and an open_long proposal linked to the
// position.
func TestClose_PositionOpenedByAProposalIsClosedBySignalOrByLink(t *testing.T) {
	t.Run("entry signal", func(t *testing.T) {
		f := newFixture(t)
		pos := f.openPositionFrom(orderproposal.PolicyVersion, 100, 2500)
		id := f.propose(closeSpec())
		f.cycle()
		f.wantEnd(id, orderproposal.StatusClosed, "")
		if p, _ := f.svc.Positions.Get(context.Background(), pos.ID); p.IsOpen() {
			t.Fatal("the position is still open")
		}
	})
	t.Run("open_long link", func(t *testing.T) {
		f := newFixture(t)
		pos := f.openPositionFrom("policy-v2", 100, 2500)
		entry := f.propose(spec{})
		if _, err := f.db.Exec(`UPDATE order_proposals SET status = 'filled', position_id = ? WHERE id = ?`, pos.ID, entry); err != nil {
			t.Fatal(err)
		}
		id := f.propose(closeSpec())
		f.cycle()
		f.wantEnd(id, orderproposal.StatusClosed, "")
		if p, _ := f.svc.Positions.Get(context.Background(), pos.ID); p.IsOpen() {
			t.Fatal("the position is still open")
		}
	})
}

// A rejected close is the Policy Engine's position seen from outside: its
// origin stays meta_model, and the rejection reads back as the fixed code.
func TestClose_RejectionIsRecordedForTheOperator(t *testing.T) {
	f := newFixture(t)
	f.openPositionFrom("policy-v2", 100, 2500)
	id := f.propose(closeSpec())
	t.Log("before:\n" + f.dump(`SELECT p.id, CASE WHEN p.closed_at IS NULL THEN 'open' ELSE 'closed' END AS status, p.exit_reason, ts.policy_version FROM positions p JOIN paper_orders o ON o.id = p.entry_order_id JOIN trade_signals ts ON ts.id = o.trade_signal_id`))
	f.cycle()
	t.Log("after:\n" + f.dump(`SELECT p.id, CASE WHEN p.closed_at IS NULL THEN 'open' ELSE 'closed' END AS status, p.exit_reason, ts.policy_version FROM positions p JOIN paper_orders o ON o.id = p.entry_order_id JOIN trade_signals ts ON ts.id = o.trade_signal_id`))
	t.Log("proposals:\n" + f.dump(`SELECT id, action, status, status_reason, position_id FROM order_proposals WHERE action='close'`))
	f.wantEnd(id, orderproposal.StatusRejected, proposalexec.ReasonPositionNotFromProposal)
}
