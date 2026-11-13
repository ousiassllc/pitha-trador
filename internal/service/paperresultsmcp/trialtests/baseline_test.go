package trialtests

import "testing"

// A slice carries the live Jev's own score of the same cohort, so an arm's rate can be read against it
// (the labels belong to the live direction), and a single arm gets no rank: a lone rank 1 reads as a winner.
func TestCompareCarriesTheLiveBaselineAndNoRankForASingleArm(t *testing.T) {
	srv, w := newServer(t)
	seedRank(t, w)
	c := compare(t, srv, period)
	var live *struct{ records, scorable, hits int }
	for _, o := range c.Outcomes {
		if o.HorizonMinutes == 5 && o.LabelVersion == 2 {
			b := o.LiveBaseline
			live = &struct{ records, scorable, hits int }{b.Records, b.Scorable, b.Hits}
			near(t, "live baseline hit rate", b.OverCohort.Rate, float64(b.Hits)/float64(b.Records))
		}
	}
	if live == nil || live.records != 20 || live.scorable != 20 {
		t.Fatalf("live baseline = %+v, want 20 scorable LONG decisions", live)
	}

	single := compare(t, srv, map[string]any{"from": "2026-10-01", "to": "2026-10-10", "arms": []string{armA}})
	for _, o := range single.Outcomes {
		for _, arm := range o.Arms {
			if arm.Rank != nil {
				t.Errorf("a single arm was given rank %d", *arm.Rank)
			}
		}
	}
}
