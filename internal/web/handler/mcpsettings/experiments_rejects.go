package mcpsettings

import (
	"cmp"
	"slices"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
	"github.com/ousiassllc/pitha-trador/internal/web/labels"
	"github.com/ousiassllc/pitha-trador/internal/web/organisms/mcp"
)

const (
	riskRejectGroup   = "Risk Engine"
	policyRejectGroup = "Policy Engine など"
)

// rejectRows joins the two periods' rejection reasons: the Risk Engine's by
// code, and the Policy Engine's own rejections by their fixed code. The codes
// are the API's; the rows are sorted by them and then shown by name.
func rejectRows(before, after *query.RiskRejections) []mcp.RejectRow {
	type key struct{ group, reason string }
	counts := map[key]*mcp.RejectRow{}
	add := func(group string, buckets []query.RejectBucket, isAfter bool) {
		for _, b := range buckets {
			row := counts[key{group, b.Reason}]
			if row == nil {
				row = &mcp.RejectRow{Group: group, Reason: b.Reason}
				counts[key{group, b.Reason}] = row
			}
			if isAfter {
				row.After = b.Count
			} else {
				row.Before = b.Count
			}
		}
	}
	for i, rr := range []*query.RiskRejections{before, after} {
		if rr != nil {
			add(riskRejectGroup, rr.Buckets, i == 1)
			add(policyRejectGroup, rr.OtherRejectBreakdown, i == 1)
		}
	}
	out := make([]mcp.RejectRow, 0, len(counts))
	for _, r := range counts {
		out = append(out, *r)
	}
	slices.SortFunc(out, func(x, y mcp.RejectRow) int {
		if x.Group != y.Group {
			return -cmp.Compare(x.Group, y.Group) // Risk Engine first
		}
		if d := (y.Before + y.After) - (x.Before + x.After); d != 0 {
			return d
		}
		return cmp.Compare(x.Reason, y.Reason)
	})
	for i := range out { // each group's codes have their own table
		if out[i].Group == policyRejectGroup {
			out[i].Reason = labels.PolicyRejectCode(out[i].Reason)
		} else {
			out[i].Reason = labels.ProposalReason(out[i].Reason)
		}
	}
	return out
}
