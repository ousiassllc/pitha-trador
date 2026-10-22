package proposaltests

import (
	"slices"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/proposal"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

// The rejection codes the application adds (issue #859 and #857) read back as
// they were recorded, in order_proposal_get (with the event's detail),
// order_proposals_list and order_proposal_summary.
func TestOrderProposalReadsShowTheProposalProcessingRejectionCodes(t *testing.T) {
	srv, w := proposalServer(t)
	const stopID, closeID = 1, 2
	testkit.Proposal(t, w, testkit.ProposalRow{ID: stopID, ReceivedAt: "2026-10-17T00:00:00Z", Status: "rejected", Reason: "stop_loss_not_below_price",
		Quantity: 100, Stop: 2600.0, MaxLoss: 20000.0})
	testkit.ProposalEvent(t, w, stopID, "2026-10-17T00:00:00Z", "", "received", "", "mcp", "")
	testkit.ProposalEvent(t, w, stopID, "2026-10-17T00:00:03Z", "received", "processing", "", "app", "")
	testkit.ProposalEvent(t, w, stopID, "2026-10-17T00:00:04Z", "processing", "rejected", "stop_loss_not_below_price", "app",
		`{"latest_price":2500,"expected_entry_price":2500,"stop_loss_price":2600}`)
	testkit.Proposal(t, w, testkit.ProposalRow{ID: closeID, Action: "close", ReceivedAt: "2026-10-17T01:00:00Z", Status: "rejected", Reason: "position_not_from_proposal"})
	testkit.ProposalEvent(t, w, closeID, "2026-10-17T01:00:00Z", "", "received", "", "mcp", "")
	testkit.ProposalEvent(t, w, closeID, "2026-10-17T01:00:03Z", "received", "processing", "", "app", "")
	testkit.ProposalEvent(t, w, closeID, "2026-10-17T01:00:04Z", "processing", "rejected", "position_not_from_proposal", "app", "")

	got := getProposal(t, srv, stopID)
	if got.Status != "rejected" || got.StatusReason != "stop_loss_not_below_price" || len(got.Events) != 3 ||
		string(got.Events[2].Detail) != `{"latest_price":2500,"expected_entry_price":2500,"stop_loss_price":2600}` {
		t.Errorf("order_proposal_get = %+v", got)
	}
	if got := getProposal(t, srv, closeID); got.Status != "rejected" || got.StatusReason != "position_not_from_proposal" {
		t.Errorf("close = %+v", got)
	}
	var out listed
	srv.call(t, "order_proposals_list", map[string]any{}).data(t, "order_proposals_list", &out)
	if len(out.Proposals) != 2 || out.Proposals[0].StatusReason != "position_not_from_proposal" || out.Proposals[1].StatusReason != "stop_loss_not_below_price" {
		t.Errorf("order_proposals_list = %+v", out.Proposals)
	}
	var s proposal.Summary
	srv.call(t, "order_proposal_summary", map[string]any{}).data(t, "order_proposal_summary", &s)
	want := []proposal.Count{{Code: "position_not_from_proposal", Count: 1}, {Code: "stop_loss_not_below_price", Count: 1}}
	if !slices.Equal(s.ByRejectReason, want) {
		t.Errorf("by_reject_reason = %+v, want %+v", s.ByRejectReason, want)
	}
}
