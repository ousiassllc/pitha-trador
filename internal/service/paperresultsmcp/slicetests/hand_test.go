package slicetests

import (
	"math"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/stats"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/evaldiag"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/slicediag"
)

// want is a bucket's expected content: the day clusters (sum, n; days ascending) of its LLM net bps
// and how many rows won (net > 0), all worked out by hand from the fixture table.
type want struct {
	bucket   string
	clusters [][2]float64
	wins     int
}

// check compares b with the hand-computed expectation; the interval and p-value are the day-cluster bootstrap
// of the hand-listed clusters (the tool must cluster by JST day, not by row).
func (w want) check(t *testing.T, what string, b slicediag.Bucket) {
	t.Helper()
	if b.Bucket != w.bucket {
		t.Errorf("%s: bucket %q, want %q", what, b.Bucket, w.bucket)
		return
	}
	var cs []stats.Cluster
	var sum, n float64
	for _, c := range w.clusters {
		cs = append(cs, stats.Cluster{Sum: c[0], N: c[1]})
		sum, n = sum+c[0], n+c[1]
	}
	if b.N != int(n) || b.Days != len(w.clusters) {
		t.Errorf("%s/%s: n=%d days=%d, want %d/%d", what, w.bucket, b.N, b.Days, int(n), len(w.clusters))
	}
	if n == 0 {
		if b.MeanNetBps != nil || b.CI != nil || b.WinRate != nil || b.PValue != nil || b.HolmAdjustedP != nil || b.DetectableMinimumEdgeBps != nil {
			t.Errorf("%s/%s: an empty bucket has numbers: %+v", what, w.bucket, b)
		}
		return
	}
	if b.MeanNetBps == nil || math.Abs(*b.MeanNetBps-sum/n) > eps {
		t.Errorf("%s/%s: mean %v, want %v", what, w.bucket, deref(b.MeanNetBps), sum/n)
	}
	if b.WinRate == nil || math.Abs(*b.WinRate-float64(w.wins)/n) > eps {
		t.Errorf("%s/%s: win rate %v, want %v", what, w.bucket, deref(b.WinRate), float64(w.wins)/n)
	}
	est := stats.Bootstrap(cs, evaldiag.Resamples, evaldiag.Alpha, evaldiag.Seed)
	if est.CI == nil {
		if b.CI != nil || b.PValue != nil || b.DetectableMinimumEdgeBps != nil {
			t.Errorf("%s/%s: one day has an interval/p/minimum: %+v", what, w.bucket, b)
		}
		return
	}
	if b.CI == nil || math.Abs(b.CI.Lower-est.CI.Lower) > eps || math.Abs(b.CI.Upper-est.CI.Upper) > eps || b.CI.Level != 0.95 {
		t.Errorf("%s/%s: ci %+v, want %+v", what, w.bucket, b.CI, est.CI)
	}
	if min := stats.DetectableMinimum(evaldiag.Alpha, evaldiag.Power, est.SE); b.DetectableMinimumEdgeBps == nil || math.Abs(*b.DetectableMinimumEdgeBps-min) > eps {
		t.Errorf("%s/%s: detectable minimum %v, want %v", what, w.bucket, deref(b.DetectableMinimumEdgeBps), min)
	}
	p, ok := stats.PValue(est.Mean, est.SE)
	if ok != (b.PValue != nil) || ok && math.Abs(*b.PValue-p) > eps {
		t.Errorf("%s/%s: p %v, want %v (defined %v)", what, w.bucket, deref(b.PValue), p, ok)
	}
}

func deref(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}

// cl is a day cluster: the sum of the LLM net bps of the day's rows and their count.
func cl(sum float64, n float64) [2]float64 { return [2]float64{sum, n} }

// Every bucket of every dimension of the small cohort against the hand-computed table (see fixture_test.go):
// counts, days, mean, win rate, day-cluster interval, minimum edge and p; the buckets are in the fixed
// order (empty ones included for the fixed dimensions) and add up to the cohort's 7 evaluated rows.
func TestBucketsMatchHandComputation(t *testing.T) {
	srv, w := newServer(t)
	seedSmall(t, w)
	d := srv.call(t, nil).data(t)
	c := cohortOf(t, d, m1, v3)
	if c.Decisions != 8 || c.Evaluated != 7 {
		t.Fatalf("decisions %d evaluated %d, want 8/7 (id 9 has no bars)", c.Decisions, c.Evaluated)
	}
	cluster := func(args ...float64) [][2]float64 {
		var out [][2]float64
		for i := 0; i < len(args); i += 2 {
			out = append(out, cl(args[i], args[i+1]))
		}
		return out
	}
	trend := cluster(30, 1, -30, 1, -20, 2)
	table := map[string][]want{
		slicediag.DimRegime: {{"trend", trend, 1}, {"mean_revert", cluster(-30, 1, -30, 1), 0}, {"unknown", cluster(30, 1), 1}},
		slicediag.DimTimeOfDay: {
			{"open", cluster(30, 1, -30, 1), 1}, {"morning", cluster(-30, 1, 0, 1), 0}, {"afternoon", cluster(-20, 1), 0},
			{"pre_close", cluster(-30, 1), 0}, {"other", cluster(30, 1), 1},
		},
		slicediag.DimDirection: {{"LONG", trend, 1}, {"SHORT", cluster(-30, 1, 30, 1, -30, 1), 1}},
		slicediag.DimConfidenceBand: {
			{"[0.0,0.6)", cluster(-30, 1), 0}, {"[0.6,0.7)", cluster(30, 1, -30, 1), 1}, {"[0.7,0.8)", cluster(-20, 1), 0},
			{"[0.8,0.9)", nil, 0}, {"[0.9,1.0]", cluster(-30, 1, 0, 1), 0}, {"unknown", cluster(30, 1), 1},
		},
		slicediag.DimWeekday: {
			{"mon", cluster(0, 2), 1}, {"tue", cluster(0, 2), 1}, {"wed", cluster(-50, 3), 0}, {"thu", nil, 0}, {"fri", nil, 0}, {"other", nil, 0},
		},
		slicediag.DimVolatilityBand: {
			{"low", cluster(30, 1, -20, 1), 1}, {"mid", cluster(-30, 1, 0, 1), 0}, {"high", cluster(-30, 1, -30, 1), 0}, {"unknown", cluster(30, 1), 1},
		},
		slicediag.DimTurnoverBand: {
			{"low", cluster(-50, 3), 0}, {"mid", cluster(0, 2), 1}, {"high", cluster(0, 2), 1}, {"unknown", nil, 0},
		},
		slicediag.DimSpreadBand: {
			{"low", cluster(0, 2, -30, 1), 1}, {"mid", cluster(30, 1, 0, 1), 1}, {"high", cluster(-50, 2), 0}, {"unknown", nil, 0},
		},
		slicediag.DimSector: {{"Auto", cluster(0, 2, -30, 1), 1}, {"Bank", cluster(30, 1, 0, 1), 1}, {"unknown", cluster(-50, 2), 0}},
	}
	seen := 0
	for name, wants := range table {
		_, dim := dimOf(t, c, name)
		if len(dim.Buckets) != len(wants) {
			t.Errorf("%s: %d buckets, want %d (%+v)", name, len(dim.Buckets), len(wants), dim.Buckets)
			continue
		}
		total := 0
		for i, x := range wants {
			x.check(t, name, dim.Buckets[i])
			total += dim.Buckets[i].N
		}
		if total != c.Evaluated || dim.Rows != c.Evaluated {
			t.Errorf("%s: buckets add up to %d (rows %d), the cohort evaluated %d", name, total, dim.Rows, c.Evaluated)
		}
		seen++
	}
	if seen != len(slicediag.Names()) || seen != 9 {
		t.Errorf("checked %d dimensions of %d", seen, len(slicediag.Names()))
	}
	want{"all", cluster(0, 2, 0, 2, -50, 3), 2}.check(t, "all", c.All)
	// The tercile edges are the first values of the mid and high thirds.
	for name, edges := range map[string][]float64{slicediag.DimVolatilityBand: {10, 20}, slicediag.DimTurnoverBand: {4, 6}, slicediag.DimSpreadBand: {4, 6}} {
		if _, dim := dimOf(t, c, name); len(dim.Edges) != 2 || dim.Edges[0] != edges[0] || dim.Edges[1] != edges[1] {
			t.Errorf("%s edges = %v, want %v", name, dim.Edges, edges)
		}
	}
	// The other cohort is cut on its own: one decision, Thu, trend, [0.8,0.9), Bank.
	o := cohortOf(t, d, m2, v4)
	_, wd := dimOf(t, o, slicediag.DimWeekday)
	_, sec := dimOf(t, o, slicediag.DimSector)
	if o.Evaluated != 1 || bucketOf(t, wd, "thu").N != 1 || bucketOf(t, wd, "mon").N != 0 || len(sec.Buckets) != 1 || bucketOf(t, sec, "Bank").N != 1 {
		t.Errorf("cohort B = %+v", o)
	}
	if len(d.Cohorts) != 2 || d.Cohorts[0].ModelID != m1 {
		t.Errorf("cohorts = %d, first %s: want the oldest cohort first", len(d.Cohorts), d.Cohorts[0].ModelID)
	}
}
