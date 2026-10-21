package proposaltests

import (
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

// order_proposal_get tells a proposal the application refused after Risk
// passed (rejected_after_risk: true, the application's code) from one the Risk
// Engine rejected and from one that reached an order (issue #860).
func TestOrderProposalGet_RejectedAfterRiskFlag(t *testing.T) {
	srv, w := proposalServer(t)
	testkit.SeedProposalOutcomes(t, w)
	testkit.SeedAfterRiskRejection(t, w)
	for _, c := range []struct {
		name         string
		id           int64
		wantCode     string
		wantAfter    bool
		wantPassedOK bool
	}{
		{"refused after Risk", testkit.ProposalRejectedAfterRisk, "size_zero", true, false},
		{"Risk Engine rejection", testkit.ProposalRejectedByRisk, "kill_switch_active", false, false},
		{"Risk Engine rejection (seeded outcome)", testkit.ProposalRejected, "bucket_budget_exceeded", false, false},
		{"reached an order", testkit.ProposalWin, "", false, true},
	} {
		s := getProposal(t, srv, c.id).Result.Signal
		if s == nil || s.RejectedAfterRisk != c.wantAfter || s.RiskPassed != c.wantPassedOK || (c.wantCode == "") != (s.RejectCode == nil) ||
			(s.RejectCode != nil && *s.RejectCode != c.wantCode) {
			t.Errorf("%s: signal = %+v, want rejected_after_risk=%v code=%q", c.name, s, c.wantAfter, c.wantCode)
		}
	}
	var raw struct {
		Result struct {
			Signal map[string]any `json:"signal"`
		} `json:"result"`
	}
	srv.call(t, "order_proposal_get", map[string]any{"proposal_id": testkit.ProposalRejectedAfterRisk}).data(t, "order_proposal_get", &raw)
	if v, ok := raw.Result.Signal["rejected_after_risk"]; !ok || v != true {
		t.Errorf("response signal = %v, want rejected_after_risk: true", raw.Result.Signal)
	}
	t.Logf("response result.signal: %v", raw.Result.Signal)
}
