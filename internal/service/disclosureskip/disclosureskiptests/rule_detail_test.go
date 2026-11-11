package disclosureskiptests

import (
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/disclosureskip"
)

// Each cause of an unknown / no_fetch is told apart (issue #1053): the detail
// is chosen from what the fetches around the decision look like and when the
// first fetch that could cover it (t+FetchLag) would have been.
func TestEvaluate_UnknownDetailTellsTheCausesApart(t *testing.T) {
	failed := func(at time.Time) disclosureskip.Fetch { return disclosureskip.Fetch{Symbol: sym, At: at.UTC()} }
	for _, tc := range []struct {
		name        string
		decision    time.Time
		fetches     []disclosureskip.Fetch
		wantReason  disclosureskip.UnknownReason
		wantDetail  disclosureskip.UnknownDetail
		wantVerdict disclosureskip.Verdict
	}{
		{
			name: "only fetches inside the lag", decision: jstAt(10, 14, 10, 0, 0),
			fetches:    []disclosureskip.Fetch{completeFetch(jstAt(10, 14, 10, 1, 0)), completeFetch(jstAt(10, 14, 10, 4, 59))},
			wantReason: disclosureskip.ReasonNoFetch, wantDetail: disclosureskip.DetailWithinFetchLag,
		},
		{
			name: "dropped out of the candidates: fetched before the decision only", decision: jstAt(10, 14, 10, 0, 0),
			fetches:    []disclosureskip.Fetch{completeFetch(jstAt(10, 14, 9, 58, 0)), completeFetch(jstAt(10, 14, 9, 59, 0))},
			wantReason: disclosureskip.ReasonNoFetch, wantDetail: disclosureskip.DetailDroppedFromCandidates,
		},
		{
			name: "no fetch at all", decision: jstAt(10, 14, 10, 0, 0),
			wantReason: disclosureskip.ReasonNoFetch, wantDetail: disclosureskip.DetailNoFetchAtAll,
		},
		{
			name: "fetches after the decision failed", decision: jstAt(10, 14, 10, 0, 0),
			fetches:    []disclosureskip.Fetch{completeFetch(jstAt(10, 14, 9, 59, 0)), failed(jstAt(10, 14, 10, 6, 0)), failed(jstAt(10, 14, 10, 20, 0))},
			wantReason: disclosureskip.ReasonNoFetch, wantDetail: disclosureskip.DetailFetchFailed,
		},
		{
			name: "a failure before the decision is not a failed fetch after it", decision: jstAt(10, 14, 10, 0, 0),
			fetches:    []disclosureskip.Fetch{failed(jstAt(10, 14, 9, 59, 0))},
			wantReason: disclosureskip.ReasonNoFetch, wantDetail: disclosureskip.DetailNoFetchAtAll,
		},
		{
			name: "lunch break: t+lag is 11:32", decision: jstAt(10, 14, 11, 27, 0),
			wantReason: disclosureskip.ReasonNoFetch, wantDetail: disclosureskip.DetailLunchBreak,
		},
		{
			name: "after the close: t+lag is 15:32", decision: jstAt(10, 14, 15, 27, 0),
			fetches:    []disclosureskip.Fetch{completeFetch(jstAt(10, 14, 15, 27, 30))},
			wantReason: disclosureskip.ReasonNoFetch, wantDetail: disclosureskip.DetailAfterClose,
		},
		{
			name: "the session time wins over the dropped-from-candidates guess", decision: jstAt(10, 14, 11, 26, 0),
			fetches:    []disclosureskip.Fetch{completeFetch(jstAt(10, 14, 11, 25, 0))},
			wantReason: disclosureskip.ReasonNoFetch, wantDetail: disclosureskip.DetailLunchBreak,
		},
		{
			name: "undated item", decision: jstAt(10, 14, 10, 0, 0),
			fetches:    []disclosureskip.Fetch{{Symbol: sym, At: jstAt(10, 14, 10, 6, 0).UTC(), OK: true, Undated: 1}},
			wantReason: disclosureskip.ReasonIncompleteFetch, wantDetail: disclosureskip.DetailUndatedItems,
		},
		{
			name: "cut by the item limit", decision: jstAt(10, 14, 10, 0, 0),
			fetches:    []disclosureskip.Fetch{{Symbol: sym, At: jstAt(10, 14, 10, 6, 0).UTC(), OK: true, Truncated: true, Oldest: jstAt(10, 14, 9, 0, 0).UTC()}},
			wantReason: disclosureskip.ReasonIncompleteFetch, wantDetail: disclosureskip.DetailItemLimit,
		},
		{
			name: "covered: no detail", decision: jstAt(10, 14, 10, 0, 0),
			fetches:     []disclosureskip.Fetch{completeFetch(jstAt(10, 14, 10, 6, 0))},
			wantVerdict: disclosureskip.VerdictNoSkip,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := disclosureskip.Evaluate(weekdaysOnly, sym, tc.decision, nil, tc.fetches)
			if tc.wantVerdict != "" {
				if got.Verdict != tc.wantVerdict || got.Reason != "" || got.Detail != "" {
					t.Fatalf("result = %+v, want %s with no reason or detail", got, tc.wantVerdict)
				}
				return
			}
			if got.Verdict != disclosureskip.VerdictUnknown || got.Reason != tc.wantReason || got.Detail != tc.wantDetail {
				t.Fatalf("result = %s / %s / %s, want unknown / %s / %s", got.Verdict, got.Reason, got.Detail, tc.wantReason, tc.wantDetail)
			}
		})
	}
}
