package proposalexectests

import (
	"context"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap/proposalexec"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/domain/orderproposal"
)

func TestProposalPastExpiresAt_IsExpiredAndNothingIsOrdered(t *testing.T) {
	f := newFixture(t)
	id := f.propose(spec{expires: at(10, 0, 30)}) // the clock reads exactly 10:00:30
	f.cycle()
	f.wantEnd(id, orderproposal.StatusExpired, proposalexec.ReasonExpired)
	if f.count("paper_orders", "") != 0 || f.count("trade_signals", "") != 0 || f.exec.enters != 0 {
		t.Fatal("an expired proposal reached Risk or Execution")
	}
}

func TestCancelRequestedBeforeProcessing_IsCancelledAndNothingIsOrdered(t *testing.T) {
	f := newFixture(t)
	id := f.propose(spec{})
	f.requestCancel(id)
	f.cycle()
	f.wantEnd(id, orderproposal.StatusCancelled, proposalexec.ReasonCancelRequested)
	if f.count("paper_orders", "") != 0 || f.exec.enters != 0 {
		t.Fatal("a cancelled proposal reached Execution")
	}
}

// Outside the session the proposal waits; when the session starts within its
// life it goes on and fills.
func TestOutsideSession_WaitsThenRunsWhenTheSessionStartsBeforeExpiry(t *testing.T) {
	f := newFixture(t)
	*f.clock = at(8, 30, 0)
	id := f.propose(spec{})
	f.cycle()
	f.wantEnd(id, orderproposal.StatusWaitingSession, proposalexec.ReasonOutsideSession)
	f.cycle()                               // still closed: nothing new is recorded
	if got := len(f.events(id)); got != 3 { // received, processing, waiting_session
		t.Fatalf("events = %v, want no event added while it keeps waiting", f.events(id))
	}
	*f.clock = at(10, 0, 30)
	f.cycle()
	f.wantEnd(id, orderproposal.StatusFilled, "")
	if f.count("paper_orders", "") != 1 {
		t.Fatalf("paper_orders = %d, want 1", f.count("paper_orders", ""))
	}
}

func TestOutsideSession_ExpiresWhenNoSessionStartsBeforeExpiry(t *testing.T) {
	f := newFixture(t)
	*f.clock = at(8, 30, 0)
	id := f.propose(spec{expires: at(8, 50, 0)})
	f.cycle()
	f.wantEnd(id, orderproposal.StatusWaitingSession, proposalexec.ReasonOutsideSession)
	*f.clock = at(8, 50, 1) // the session (9:00) never came before expires_at
	f.cycle()
	f.wantEnd(id, orderproposal.StatusExpired, proposalexec.ReasonExpired)
	if f.count("paper_orders", "") != 0 || f.exec.enters != 0 {
		t.Fatal("an order went out although no session started before expiry")
	}
}

func TestWaitingProposal_CancelRequestedIsCancelled(t *testing.T) {
	f := newFixture(t)
	*f.clock = at(8, 30, 0)
	id := f.propose(spec{})
	f.cycle()
	f.requestCancel(id)
	f.cycle()
	f.wantEnd(id, orderproposal.StatusCancelled, proposalexec.ReasonCancelRequested)
}

// A limit order that has not filled by expires_at is cancelled and the
// proposal expires.
func TestPendingLimitOrder_IsCancelledAtExpiry(t *testing.T) {
	f := newFixture(t)
	id := f.propose(spec{orderType: domain.OrderTypeLimit, limit: 2000, expires: at(10, 5, 0)})
	f.cycle()
	p := f.wantEnd(id, orderproposal.StatusSubmitted, "")
	if p.PaperOrderID == nil || p.PositionID != nil {
		t.Fatalf("links = order %v position %v, want an order and no position", p.PaperOrderID, p.PositionID)
	}
	f.cycle() // still within its life: unchanged
	f.wantEnd(id, orderproposal.StatusSubmitted, "")

	*f.clock = at(10, 5, 0)
	f.cycle()
	f.wantEnd(id, orderproposal.StatusExpired, proposalexec.ReasonExpired)
	if order, _ := f.svc.Orders.Get(context.Background(), *p.PaperOrderID); order.Status != domain.OrderStatusCancelled {
		t.Fatalf("order status = %s, want CANCELLED", order.Status)
	}
}

func TestSubmittedProposal_CancelRequestedCancelsThePendingOrder(t *testing.T) {
	f := newFixture(t)
	id := f.propose(spec{orderType: domain.OrderTypeLimit, limit: 2000})
	f.cycle()
	p := f.wantEnd(id, orderproposal.StatusSubmitted, "")
	f.requestCancel(id)
	f.cycle()
	f.wantEnd(id, orderproposal.StatusCancelled, proposalexec.ReasonCancelRequested)
	if order, _ := f.svc.Orders.Get(context.Background(), *p.PaperOrderID); order.Status != domain.OrderStatusCancelled {
		t.Fatalf("order status = %s, want CANCELLED", order.Status)
	}
}

// The order fills through Execution's own per-bar step; the proposal follows.
func TestSubmittedProposal_BecomesFilledWhenTheLimitOrderFills(t *testing.T) {
	f := newFixture(t)
	id := f.propose(spec{orderType: domain.OrderTypeLimit, limit: 2400})
	f.cycle()
	p := f.wantEnd(id, orderproposal.StatusSubmitted, "")
	snap := domain.Snapshot{InstrumentID: f.inst.ID, Symbol: f.inst.Symbol, Timestamp: at(10, 0, 20), Price: 2390, SpreadBps: new(float64), RawDataJSON: "{}"}
	if _, err := f.svc.Execution.OnSnapshot(context.Background(), snap); err != nil {
		t.Fatal(err)
	}
	f.cycle()
	got := f.wantEnd(id, orderproposal.StatusFilled, "")
	if got.PositionID == nil || got.PaperOrderID == nil || *got.PaperOrderID != *p.PaperOrderID {
		t.Fatalf("links = %+v, want the position of order %d", got, *p.PaperOrderID)
	}
}

// The limit order filled and the proposal's time ran out before the processor
// looked: what the order became wins. The order is not turned CANCELLED under
// its open position and the proposal is filled, not expired.
func TestSubmittedProposal_FilledOrderIsNotCancelledByExpiry(t *testing.T) {
	f := newFixture(t)
	id := f.propose(spec{orderType: domain.OrderTypeLimit, limit: 2400, expires: at(10, 5, 0)})
	f.cycle()
	p := f.wantEnd(id, orderproposal.StatusSubmitted, "")
	snap := domain.Snapshot{InstrumentID: f.inst.ID, Symbol: f.inst.Symbol, Timestamp: at(10, 0, 20), Price: 2390, SpreadBps: new(float64), RawDataJSON: "{}"}
	if _, err := f.svc.Execution.OnSnapshot(context.Background(), snap); err != nil {
		t.Fatal(err)
	}
	*f.clock = at(10, 6, 0) // past expires_at
	f.cycle()
	got := f.wantEnd(id, orderproposal.StatusFilled, "")
	if order, _ := f.svc.Orders.Get(context.Background(), *p.PaperOrderID); order.Status != domain.OrderStatusFilled || got.PositionID == nil {
		t.Fatalf("order = %s, position link = %v; want FILLED with its position", order.Status, got.PositionID)
	}
}

// A cancellation request that lands after the processor read the proposal and
// before it claimed it must still win: nothing is ordered for it.
func TestCancelRequestedBetweenReadAndClaim_IsCancelledAndNothingIsOrdered(t *testing.T) {
	f := newFixture(t)
	id := f.propose(spec{})
	f.exec.beforeClaim = func() { f.requestCancel(id) }
	f.cycle()
	f.wantEnd(id, orderproposal.StatusCancelled, proposalexec.ReasonCancelRequested)
	if f.count("paper_orders", "") != 0 || f.count("trade_signals", "") != 0 || f.exec.enters != 0 {
		t.Fatal("a proposal cancelled while being claimed reached Risk or Execution")
	}
}

// The session can close between the processor's check and Execution's own: the
// proposal then waits for the next session (as the document says) instead of
// being rejected.
func TestOpenLong_SessionClosingBeforeTheOrderWaitsForTheNextSession(t *testing.T) {
	f := newFixture(t)
	*f.clock = at(8, 30, 0)
	open := true
	f.exec.sessionOpen = &open
	id := f.propose(spec{})
	f.cycle()
	f.wantEnd(id, orderproposal.StatusWaitingSession, proposalexec.ReasonOutsideSession)
	if f.count("paper_orders", "") != 0 {
		t.Fatal("an order went out outside the session")
	}
	f.exec.sessionOpen = nil
	*f.clock = at(10, 0, 30)
	f.cycle()
	f.wantEnd(id, orderproposal.StatusFilled, "")
}
