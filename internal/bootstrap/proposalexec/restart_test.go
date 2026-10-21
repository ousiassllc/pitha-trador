package proposalexec_test

import (
	"context"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/domain/orderproposal"
)

// stopMidFlight puts a proposal back to processing, the state an app stopped
// in the middle of the flow leaves behind, keeping whatever links it had.
func (f *fixture) stopMidFlight(id int64) {
	f.t.Helper()
	if _, err := f.db.Exec(`UPDATE order_proposals SET status = 'processing', status_reason = '', paper_order_id = NULL, position_id = NULL WHERE id = ?`, id); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) recover() int {
	f.t.Helper()
	n, err := f.proc.Recover(context.Background())
	if err != nil {
		f.t.Fatalf("Recover: %v", err)
	}
	return n
}

// The app stopped after the order went out but before the proposal was
// updated: after the restart the proposal takes the order's outcome and no
// second order is placed.
func TestRestart_ProposalWhoseOrderWentOutIsNotOrderedTwice(t *testing.T) {
	f := newFixture(t)
	id := f.propose(spec{quantity: 100})
	f.cycle()
	first := f.wantEnd(id, orderproposal.StatusFilled, "")
	f.stopMidFlight(id) // keeps trade_signal_id; the order and the position exist

	if n := f.recover(); n != 1 {
		t.Fatalf("Recover = %d, want 1", n)
	}
	got := f.wantEnd(id, orderproposal.StatusFilled, "")
	if got.PaperOrderID == nil || *got.PaperOrderID != *first.PaperOrderID || got.PositionID == nil || *got.PositionID != *first.PositionID {
		t.Fatalf("recovered links = order %v position %v, want the first run's order %d and position %d", got.PaperOrderID, got.PositionID, *first.PaperOrderID, *first.PositionID)
	}
	f.cycle()
	f.cycle()
	if n, enters := f.count("paper_orders", ""), f.exec.enters; n != 1 || enters != 1 {
		t.Fatalf("paper_orders = %d, Enter calls = %d; want 1 and 1 (one order for one proposal)", n, enters)
	}
	t.Logf("restart events: %v", f.events(id))
}

// The app stopped before any order: the proposal goes back to received and is
// processed again, once.
func TestRestart_ProposalWithoutAnOrderIsProcessedAgainOnce(t *testing.T) {
	f := newFixture(t)
	id := f.propose(spec{quantity: 100})
	if _, err := f.db.Exec(`UPDATE order_proposals SET status = 'processing' WHERE id = ?`, id); err != nil {
		t.Fatal(err)
	}
	f.recover()
	f.wantEnd(id, orderproposal.StatusReceived, "recovered_after_restart")
	f.cycle()
	f.wantEnd(id, orderproposal.StatusFilled, "")
	f.recover() // nothing is stuck any more
	f.cycle()
	if n := f.count("paper_orders", ""); n != 1 {
		t.Fatalf("paper_orders = %d, want 1", n)
	}
}

// A limit order that was placed and still waits: the proposal becomes submitted.
func TestRestart_ProposalWhoseLimitOrderIsWaitingBecomesSubmitted(t *testing.T) {
	f := newFixture(t)
	id := f.propose(spec{orderType: domain.OrderTypeLimit, limit: 2000})
	f.cycle()
	first := f.wantEnd(id, orderproposal.StatusSubmitted, "")
	f.stopMidFlight(id)
	f.recover()
	got := f.wantEnd(id, orderproposal.StatusSubmitted, "")
	if got.PaperOrderID == nil || *got.PaperOrderID != *first.PaperOrderID || f.count("paper_orders", "") != 1 {
		t.Fatalf("order = %v, paper_orders = %d; want the same single order", got.PaperOrderID, f.count("paper_orders", ""))
	}
}

// A close the app stopped around: if the position is already closed by it, the
// proposal is closed; if not, the close runs again.
func TestRestart_CloseProposalFollowsWhatHappenedToThePosition(t *testing.T) {
	f := newFixture(t)
	pos := f.openPosition(100, 2500)
	id := f.propose(closeSpec())
	f.cycle()
	f.wantEnd(id, orderproposal.StatusClosed, "")
	if _, err := f.db.Exec(`UPDATE order_proposals SET status = 'processing', status_reason = '', position_id = NULL, paper_order_id = NULL WHERE id = ?`, id); err != nil {
		t.Fatal(err)
	}
	f.recover()
	got := f.wantEnd(id, orderproposal.StatusClosed, "")
	if got.PositionID == nil || *got.PositionID != pos.ID {
		t.Fatalf("recovered position = %v, want %d", got.PositionID, pos.ID)
	}
	if n := f.count("paper_orders", "WHERE side = 'SELL'"); n != 1 {
		t.Fatalf("exit orders = %d, want 1 (the close was not repeated)", n)
	}
}
