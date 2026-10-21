package proposalexec_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// signalsOfProposals is the evidence query of #860: each proposal's signal next to the proposal's end state.
const signalsOfProposals = `SELECT ts.id, ts.risk_passed, ts.reject_reason, op.status, op.status_reason, op.paper_order_id
FROM trade_signals ts JOIN order_proposals op ON op.trade_signal_id = ts.id ORDER BY ts.id`

// rejections reads risk_rejections' data over the day of the fixture's clock.
func (f *fixture) rejections() query.RiskRejections {
	f.t.Helper()
	r, err := query.Rejections(context.Background(), f.db, query.Range{From: at(0, 0, 0), To: at(0, 0, 0).Add(24 * time.Hour)})
	if err != nil {
		f.t.Fatal(err)
	}
	out, _ := json.Marshal(r)
	f.t.Logf("risk_rejections: %s", out)
	return r
}

func breakdown(b []query.RejectBucket) string {
	var parts []string
	for _, x := range b {
		parts = append(parts, fmt.Sprintf("%s=%d", x.Reason, x.Count))
	}
	return strings.Join(parts, ",")
}

// A proposal that passed Risk but was refused afterwards (size_zero,
// max_trade_loss, position_already_open) is not a Risk pass: its signal is
// risk_passed = 0 with 'proposal_rejected_after_risk: <code>', and
// risk_rejections counts it in its own count and breakdown, apart from the
// Policy Engine's other_reject_count. A proposal that reached an order (filled,
// or waiting on the book) still counts as passed.
func TestRiskRejections_PostRiskProposalRejectionsAreNotRiskPassed(t *testing.T) {
	f := newFixture(t)
	f.propose(spec{budget: 100_000}) // size_zero
	f.propose(spec{stop: 500})       // max_trade_loss
	f.propose(spec{quantity: 100})   // filled
	f.propose(spec{})                // position_already_open
	f.cycle()
	t.Logf("signals before the fix would read risk_passed=1 for all four; now:\n%s", f.dump(signalsOfProposals))

	r := f.rejections()
	if r.RiskPassedCount != 1 || r.ProposalRiskPassedCount != 1 {
		t.Fatalf("risk_passed_count = %d, proposal_risk_passed_count = %d; want 1 and 1 (only the filled proposal)", r.RiskPassedCount, r.ProposalRiskPassedCount)
	}
	if got, want := breakdown(r.ProposalRejectedAfterRiskBreakdown), "max_trade_loss=1,position_already_open=1,size_zero=1"; r.ProposalRejectedAfterRiskCount != 3 || got != want {
		t.Fatalf("proposal_rejected_after_risk = %d (%s), want 3 (%s)", r.ProposalRejectedAfterRiskCount, got, want)
	}
	if r.OtherRejectCount != 0 || len(r.OtherRejectBreakdown) != 0 || r.ProposalRiskRejectedCount != 0 {
		t.Fatalf("other_reject_count = %d (%v), proposal_risk_rejected_count = %d (the Risk Engine's own rejections only); want 0, none, 0", r.OtherRejectCount, r.OtherRejectBreakdown, r.ProposalRiskRejectedCount)
	}

	// A limit order waiting on the book is an order: still a Risk pass.
	g := newFixture(t)
	g.propose(spec{orderType: domain.OrderTypeLimit, limit: 2000})
	g.cycle()
	if r := g.rejections(); r.RiskPassedCount != 1 || r.ProposalRejectedAfterRiskCount != 0 {
		t.Fatalf("waiting limit order: risk_passed_count = %d, after-risk = %d; want 1 and 0", r.RiskPassedCount, r.ProposalRejectedAfterRiskCount)
	}
}

func (f *fixture) mustExec(q string, args ...any) {
	f.t.Helper()
	if _, err := f.db.Exec(q, args...); err != nil {
		f.t.Fatal(err)
	}
}
