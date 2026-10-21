package origin_test

import (
	"database/sql"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/domain/orderproposal"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/origin"
	"github.com/ousiassllc/pitha-trador/internal/service/policy"
	"github.com/ousiassllc/pitha-trador/internal/service/policy/paperbootstrap"
)

// The origin follows the policy_version the Policy Engine and the proposal
// processor really write, applied-proposal suffix included.
func TestOfFollowsThePolicyVersionsTheAppWrites(t *testing.T) {
	v := func(s string) sql.NullString { return sql.NullString{String: s, Valid: true} }
	for _, tt := range []struct {
		version sql.NullString
		want    string
	}{
		{v(policy.MetaVersion), origin.MetaModel},
		{v(policy.MetaVersion + "+sol-12"), origin.MetaModel},
		{v(domain.PolicyVersionPaperBootstrap), origin.Bootstrap},
		{v(paperbootstrap.PolicyVersion(policy.MetaVersion+"+sol-12", policy.MetaVersion)), origin.Bootstrap},
		{v(orderproposal.PolicyVersion), origin.OrderProposal},
		{v("some-older-version"), origin.MetaModel},
		{sql.NullString{}, origin.Unknown},
	} {
		if got := origin.Of(tt.version); got != tt.want {
			t.Errorf("Of(%+v) = %q, want %q", tt.version, got, tt.want)
		}
	}
}

func TestTallyListsEveryOriginInOrder(t *testing.T) {
	var tally origin.Tally
	tally.Add(origin.OrderProposal, 100)
	tally.Add(origin.OrderProposal, -40)
	tally.Add(origin.OrderProposal, 0)
	stats := tally.Stats()
	var got []string
	for _, s := range stats {
		got = append(got, s.Origin)
	}
	want := origin.All()
	if len(got) != len(want) {
		t.Fatalf("origins = %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("origins = %v, want %v", got, want)
		}
	}
	p := stats[2]
	if p.ClosedTradeCount != 3 || p.WinCount != 1 || p.LossCount != 1 || p.TotalPnlJPY != 60 || p.WinRate == nil || *p.WinRate != 1.0/3 || *p.ExpectancyJPY != 20 {
		t.Errorf("order_proposal = %+v", p)
	}
	if stats[0].WinRate != nil || stats[0].ExpectancyJPY != nil {
		t.Errorf("an origin with no trade has no ratios: %+v", stats[0])
	}
}
