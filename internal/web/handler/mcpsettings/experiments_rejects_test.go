package mcpsettings

import (
	"slices"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
	"github.com/ousiassllc/pitha-trador/internal/web/labels"
	"github.com/ousiassllc/pitha-trador/internal/web/organisms/mcp"
)

func TestRejectRowsJoinsPeriodsByGroupAndCode(t *testing.T) {
	before := &query.RiskRejections{
		Buckets: []query.RejectBucket{{Reason: "size_zero", Count: 2}, {Reason: "max_trade_loss", Count: 1}},
		OtherRejectBreakdown: []query.RejectBucket{
			{Reason: "jev_none", Count: 3}, {Reason: "other", Count: 1},
		},
	}
	after := &query.RiskRejections{
		Buckets: []query.RejectBucket{{Reason: "size_zero", Count: 5}},
		OtherRejectBreakdown: []query.RejectBucket{
			{Reason: "jev_none", Count: 1}, {Reason: "other", Count: 3}, {Reason: "meta_skip:no_active_model", Count: 9},
		},
	}

	got := rejectRows(before, after)

	want := []mcp.RejectRow{
		// The Risk Engine's group comes first, its busiest code first.
		{Group: riskRejectGroup, Reason: labels.ProposalReason("size_zero"), Before: 2, After: 5},
		{Group: riskRejectGroup, Reason: labels.ProposalReason("max_trade_loss"), Before: 1, After: 0},
		// The Policy Engine's codes are named by their own table; a code seen in
		// one period only has 0 in the other; equal totals are ordered by code.
		{Group: policyRejectGroup, Reason: labels.PolicyRejectCode("meta_skip:no_active_model"), Before: 0, After: 9},
		{Group: policyRejectGroup, Reason: labels.PolicyRejectCode("jev_none"), Before: 3, After: 1},
		{Group: policyRejectGroup, Reason: labels.PolicyRejectCode("other"), Before: 1, After: 3},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("rejectRows =\n%+v\nwant\n%+v", got, want)
	}
}

// The same code in the two groups is two rows: "other" of the Policy Engine
// must not merge with a Risk Engine code, and each is named by its own table.
func TestRejectRowsKeepsGroupsApart(t *testing.T) {
	rr := &query.RiskRejections{
		Buckets:              []query.RejectBucket{{Reason: "other", Count: 1}},
		OtherRejectBreakdown: []query.RejectBucket{{Reason: "other", Count: 2}},
	}

	got := rejectRows(rr, nil)

	want := []mcp.RejectRow{
		{Group: riskRejectGroup, Reason: labels.ProposalReason("other"), Before: 1},
		{Group: policyRejectGroup, Reason: labels.PolicyRejectCode("other"), Before: 2},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("rejectRows =\n%+v\nwant\n%+v", got, want)
	}
}

func TestRejectRowsWithoutData(t *testing.T) {
	if got := rejectRows(nil, nil); len(got) != 0 {
		t.Fatalf("rejectRows(nil, nil) = %+v, want none", got)
	}
	if got := rejectRows(&query.RiskRejections{}, &query.RiskRejections{}); len(got) != 0 {
		t.Fatalf("rejectRows(empty, empty) = %+v, want none", got)
	}
	got := rejectRows(nil, &query.RiskRejections{Buckets: []query.RejectBucket{{Reason: "size_zero", Count: 4}}})
	want := []mcp.RejectRow{{Group: riskRejectGroup, Reason: labels.ProposalReason("size_zero"), After: 4}}
	if !slices.Equal(got, want) {
		t.Fatalf("rejectRows(nil, after) = %+v, want %+v", got, want)
	}
}
