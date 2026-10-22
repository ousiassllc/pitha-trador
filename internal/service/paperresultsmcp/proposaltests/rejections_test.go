package proposaltests

import (
	"fmt"
	"slices"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/domain/orderproposal"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

// risk_rejections counts the rejections of order proposals (issue #828) like
// any other Risk rejection, and tells them apart by policy_version: the
// proposal_* counts are the signals of mcp-proposal-v1.
func TestRiskRejectionsCountOrderProposalsByPolicyVersion(t *testing.T) {
	srv, w := proposalServer(t)
	testkit.MustExec(t, w, `INSERT INTO instruments (id, symbol, name, market) VALUES (1, '7203', 'n', 'TSE')`)
	for _, s := range []struct {
		version string
		passed  int
		reason  any
	}{
		{orderproposal.PolicyVersion, 1, nil},
		{orderproposal.PolicyVersion, 0, "risk_engine_rejected: kill_switch_active: [operator_manual]"},
		{orderproposal.PolicyVersion, 0, "risk_engine_rejected: bucket_disabled: bucket=stock budget_jpy=0"},
		{"policy-v1", 0, "risk_engine_rejected: kill_switch_active: [operator_manual]"},
		{"policy-v1", 1, nil},
		{"policy-v1", 0, "jev_none"},
	} {
		testkit.MustExec(t, w, `INSERT INTO trade_signals (instrument_id, symbol, timestamp, direction, policy_version, risk_passed, reject_reason)
			VALUES (1, '7203', ?, 'LONG', ?, ?, ?)`, testkit.Stored(t, "2026-10-01T00:00:00Z"), s.version, s.passed, s.reason)
	}
	var d struct {
		RiskPassed       int `json:"risk_passed_count"`
		OtherRejectCount int `json:"other_reject_count"`
		ProposalPassed   int `json:"proposal_risk_passed_count"`
		ProposalRejected int `json:"proposal_risk_rejected_count"`
		Buckets          []struct {
			Reason string `json:"reason"`
			Count  int    `json:"count"`
		} `json:"buckets"`
	}
	srv.call(t, "risk_rejections", map[string]any{"from": "2026-10-01", "to": "2026-10-10"}).data(t, "risk_rejections", &d)
	if d.RiskPassed != 2 || d.OtherRejectCount != 1 || d.ProposalPassed != 1 || d.ProposalRejected != 2 {
		t.Fatalf("passed=%d other=%d proposal passed=%d proposal rejected=%d, want 2 / 1 / 1 / 2", d.RiskPassed, d.OtherRejectCount, d.ProposalPassed, d.ProposalRejected)
	}
	var got []string
	for _, b := range d.Buckets {
		got = append(got, fmt.Sprintf("%s=%d", b.Reason, b.Count))
	}
	if want := []string{"kill_switch_active=2", "bucket_disabled=1"}; !slices.Equal(got, want) {
		t.Fatalf("buckets = %v, want %v (the proposals' Risk rejections count like the Policy Engine's)", got, want)
	}
}

// A proposal that passed Risk but did not become an order (issue #860) has a
// signal with risk_passed = 0 and 'proposal_rejected_after_risk: <code>'. It is
// counted in proposal_rejected_after_risk_count and its breakdown, and in none
// of risk_passed_count, buckets, other_reject_count and other_reject_breakdown.
func TestRiskRejections_PostRiskProposalRejectionsAreCountedApart(t *testing.T) {
	srv, w := proposalServer(t)
	testkit.MustExec(t, w, `INSERT INTO instruments (id, symbol, name, market) VALUES (1, '7203', 'n', 'TSE')`)
	for _, s := range []struct {
		version string
		passed  int
		reason  any
	}{
		{orderproposal.PolicyVersion, 1, nil}, // filled or waiting on the book: a Risk pass
		{orderproposal.PolicyVersion, 0, "proposal_rejected_after_risk: size_zero"},
		{orderproposal.PolicyVersion, 0, "proposal_rejected_after_risk: size_zero"},
		{orderproposal.PolicyVersion, 0, "proposal_rejected_after_risk: max_trade_loss"},
		{orderproposal.PolicyVersion, 0, "proposal_rejected_after_risk: position_already_open"},
		{orderproposal.PolicyVersion, 0, "risk_engine_rejected: kill_switch_active: [operator_manual]"},
		{"policy-v1", 0, "jev_none"},
	} {
		testkit.MustExec(t, w, `INSERT INTO trade_signals (instrument_id, symbol, timestamp, direction, policy_version, risk_passed, reject_reason)
			VALUES (1, '7203', ?, 'LONG', ?, ?, ?)`, testkit.Stored(t, "2026-10-01T00:00:00Z"), s.version, s.passed, s.reason)
	}
	args := map[string]any{"from": "2026-10-01", "to": "2026-10-10"}
	var raw map[string]any
	resp := srv.call(t, "risk_rejections", args)
	resp.data(t, "risk_rejections", &raw)
	t.Logf("request: %v\nresponse data: %v", callBody("risk_rejections", args), raw)
	str := func(b []any) (out []string) {
		for _, x := range b {
			m := x.(map[string]any)
			out = append(out, fmt.Sprintf("%s=%v", m["reason"], m["count"]))
		}
		return
	}
	num := func(k string) int { return int(raw[k].(float64)) }
	if num("risk_passed_count") != 1 || num("proposal_risk_passed_count") != 1 || num("other_reject_count") != 1 ||
		num("proposal_risk_rejected_count") != 1 || num("proposal_rejected_after_risk_count") != 4 {
		t.Fatalf("counts = %v, want passed 1, proposal passed 1, other 1, proposal Risk rejected 1, after-risk 4", raw)
	}
	for key, want := range map[string][]string{
		"proposal_rejected_after_risk_breakdown": {"size_zero=2", "max_trade_loss=1", "position_already_open=1"},
		"buckets":                                {"kill_switch_active=1"},
		"other_reject_breakdown":                 {"jev_none=1"},
	} {
		if got := str(raw[key].([]any)); !slices.Equal(got, want) {
			t.Errorf("%s = %v, want %v", key, got, want)
		}
	}
}
