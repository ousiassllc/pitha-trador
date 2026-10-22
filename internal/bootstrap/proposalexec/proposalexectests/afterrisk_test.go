package proposalexectests

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/domain/orderproposal"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// signalsOfProposals is the evidence query of #860: each proposal's signal next to the proposal's end state.
const signalsOfProposals = `SELECT ts.id, ts.risk_passed, ts.reject_reason, op.status, op.status_reason, op.paper_order_id
FROM trade_signals ts JOIN order_proposals op ON op.trade_signal_id = ts.id ORDER BY ts.id`

func (f *fixture) rejections() query.RiskRejections {
	f.t.Helper()
	r, err := query.Rejections(context.Background(), f.db, query.Range{From: at(0, 0, 0), To: at(0, 0, 0).Add(24 * time.Hour)})
	if err != nil {
		f.t.Fatal(err)
	}
	return r
}

// Passed Risk but refused afterwards (size_zero, max_trade_loss,
// position_already_open): not a Risk pass; counted apart from other_reject_count.
func TestRiskRejections_PostRiskProposalRejectionsAreNotRiskPassed(t *testing.T) {
	f := newFixture(t)
	f.propose(spec{budget: 100_000}) // size_zero
	f.propose(spec{stop: 500})       // max_trade_loss
	f.propose(spec{quantity: 100})   // filled
	f.propose(spec{})                // position_already_open
	f.cycle()
	r := f.rejections()
	if r.RiskPassedCount != 1 || r.ProposalRiskPassedCount != 1 {
		t.Fatalf("risk_passed_count = %d, proposal_risk_passed_count = %d; want 1 and 1 (only the filled proposal)", r.RiskPassedCount, r.ProposalRiskPassedCount)
	}
	if got, want := fmt.Sprint(r.ProposalRejectedAfterRiskBreakdown), "[{max_trade_loss 1} {position_already_open 1} {size_zero 1}]"; r.ProposalRejectedAfterRiskCount != 3 || got != want {
		t.Fatalf("proposal_rejected_after_risk = %d (%s), want 3 (%s)", r.ProposalRejectedAfterRiskCount, got, want)
	}
	if r.OtherRejectCount != 0 || len(r.OtherRejectBreakdown) != 0 || r.ProposalRiskRejectedCount != 0 {
		t.Fatalf("other_reject_count = %d (%v), proposal_risk_rejected_count = %d (the Risk Engine's own rejections only); want 0, none, 0", r.OtherRejectCount, r.OtherRejectBreakdown, r.ProposalRiskRejectedCount)
	}
}

func (f *fixture) mustExec(q string, args ...any) {
	f.t.Helper()
	if _, err := f.db.Exec(q, args...); err != nil {
		f.t.Fatal(err)
	}
}

// An ordered proposal stays risk_passed = 1 when its limit order is later cancelled or expires.
func TestRiskRejections_OrderedProposalStaysRiskPassedAfterCancelOrExpiry(t *testing.T) {
	f := newFixture(t)
	cancelled := f.propose(spec{orderType: domain.OrderTypeLimit, limit: 2000})
	f.cycle()
	f.requestCancel(cancelled)
	f.cycle()
	f.wantEnd(cancelled, "cancelled", "cancel_requested")
	expired := f.propose(spec{orderType: domain.OrderTypeLimit, limit: 2000, expires: at(10, 5, 0)})
	f.cycle()
	*f.clock = at(10, 5, 0)
	f.cycle()
	f.wantEnd(expired, "expired", "expired")
	if n := f.count("trade_signals", "WHERE risk_passed = 1 AND reject_reason IS NULL"); n != 2 {
		t.Fatalf("%d signals still risk_passed=1 without a reason, want 2:\n%s", n, f.dump(signalsOfProposals))
	}
	if r := f.rejections(); r.RiskPassedCount != 2 || r.ProposalRejectedAfterRiskCount != 0 {
		t.Fatalf("risk_passed_count = %d, after-risk = %d; want 2 and 0", r.RiskPassedCount, r.ProposalRejectedAfterRiskCount)
	}
}

// A retry writes a new signal; the old one is rewritten when the proposal goes
// back to waiting, and that event keeps its id (issue #860).
func TestRetry_OldSignalIsNotLeftRiskPassedWithoutAnOrder(t *testing.T) {
	f := newFixture(t)
	open := true
	f.exec.sessionOpen = &open // Enter judges the clock, which is before the session
	inSession := *f.clock
	*f.clock = at(8, 0, 0)
	id := f.propose(spec{quantity: 100})
	f.cycle()
	f.wantEnd(id, orderproposal.StatusWaitingSession, "outside_trading_session")
	first := *f.get(id).TradeSignalID

	f.exec.sessionOpen, *f.clock = nil, inSession
	f.cycle()
	f.wantEnd(id, orderproposal.StatusFilled, "")
	if n := f.count("trade_signals", "WHERE risk_passed = 1 AND id NOT IN (SELECT trade_signal_id FROM paper_orders WHERE trade_signal_id IS NOT NULL)"); n != 0 {
		t.Fatalf("%d signals are risk_passed = 1 without an order:\n%s", n, f.dump(`SELECT id, risk_passed, reject_reason FROM trade_signals`))
	}
	events := f.dump(`SELECT detail_json FROM order_proposal_events WHERE proposal_id = ` + strconv.FormatInt(id, 10) + ` AND to_status = 'waiting_session'`)
	if !strings.Contains(events, `"trade_signal_id":`+strconv.FormatInt(first, 10)) {
		t.Fatalf("the waiting event lacks the old signal id %d:\n%s", first, events)
	}
}
