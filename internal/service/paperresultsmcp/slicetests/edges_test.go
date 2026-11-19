package slicetests

import (
	"database/sql"
	"fmt"
	"testing"
)

// countsOf is the n of each bucket of a dimension of the only cohort, in output order, as "name:n".
func countsOf(t *testing.T, srv *testServer, dim string) []string {
	t.Helper()
	d := srv.call(t, nil).data(t)
	if len(d.Cohorts) != 1 {
		t.Fatalf("cohorts = %d", len(d.Cohorts))
	}
	c := d.Cohorts[0]
	_, x := dimOf(t, c, dim)
	total := 0
	var out []string
	for _, b := range x.Buckets {
		out = append(out, fmt.Sprintf("%s:%d", b.Bucket, b.N))
		total += b.N
	}
	if total != c.Evaluated {
		t.Errorf("%s adds up to %d rows, the cohort evaluated %d", dim, total, c.Evaluated)
	}
	return out
}

func equalLines(t *testing.T, what string, got, want []string) {
	t.Helper()
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("%s = %v, want %v", what, got, want)
	}
}

// onePerInstrument seeds one LONG "up" decision per spec on its own instrument (several decisions a minute apart
// would share bars), cohort model-1/trader-v3.
func onePerInstrument(t *testing.T, w *sql.DB, specs []spec) {
	t.Helper()
	sectors := map[int]string{}
	for i := range specs {
		specs[i].id, specs[i].inst, specs[i].dir, specs[i].scenario, specs[i].model, specs[i].version = i+1, i+1, "LONG", "up", m1, v3
		sectors[i+1] = ""
	}
	seed(t, w, sectors, specs)
}

// The time-of-day edges are half-open in JST: 09:00 and 09:29:59 are open, 09:30 morning, 11:29:59 morning, 11:30 and
// the lunch break are other, 12:30 afternoon, 14:30 pre_close, 15:29:59 pre_close and 15:30 other.
func TestTimeOfDayEdgesAreHalfOpenInJST(t *testing.T) {
	srv, w := newServer(t)
	var specs []spec
	for _, clock := range []string{"08:59:59", "09:00:00", "09:29:59", "09:30:00", "11:29:59", "11:30:00", "12:29:59", "12:30:00", "14:29:59", "14:30:00", "15:29:59", "15:30:00"} {
		specs = append(specs, spec{at: jst(t, "2026-10-06T"+clock)})
	}
	onePerInstrument(t, w, specs)
	equalLines(t, "time_of_day", countsOf(t, srv, "time_of_day"), []string{"open:2", "morning:2", "afternoon:2", "pre_close:2", "other:4"})
}

// The confidence bands are [0,0.6) [0.6,0.7) [0.7,0.8) [0.8,0.9) [0.9,1]: a lower edge belongs to the band above, 1.0 to the last
// band; nothing recorded, above 1 and below 0 are unknown.
func TestConfidenceBandEdges(t *testing.T) {
	srv, w := newServer(t)
	var specs []spec
	for _, c := range []*float64{f(0), f(0.5999), f(0.6), f(0.6999), f(0.7), f(0.7999), f(0.8), f(0.8999), f(0.9), f(1), f(1.5), f(-0.1), nil} {
		specs = append(specs, spec{at: jst(t, "2026-10-06T10:00:00"), conf: c})
	}
	onePerInstrument(t, w, specs)
	equalLines(t, "confidence_band", countsOf(t, srv, "confidence_band"),
		[]string{"[0.0,0.6):2", "[0.6,0.7):2", "[0.7,0.8):2", "[0.8,0.9):2", "[0.9,1.0]:2", "unknown:3"})
}

// The weekday is the JST one (a decision at 08:30 JST on Saturday is Friday 23:30 UTC); a weekend day is other; the regime is
// the recorded one and anything else (not recorded, or a value that is not trend / mean_revert) is unknown.
func TestWeekdayAndRegimeUseTheirOwnRules(t *testing.T) {
	srv, w := newServer(t)
	var specs []spec
	for _, day := range []string{"2026-10-09", "2026-10-10", "2026-10-11"} {
		specs = append(specs, spec{at: jst(t, day+"T08:30:00")}, spec{at: jst(t, day+"T10:00:00"), regime: "unknown"})
	}
	onePerInstrument(t, w, specs)
	equalLines(t, "weekday", countsOf(t, srv, "weekday"), []string{"mon:0", "tue:0", "wed:0", "thu:0", "fri:2", "other:4"})
	equalLines(t, "regime", countsOf(t, srv, "regime"), []string{"trend:0", "mean_revert:0", "unknown:6"})
}

// The terciles are by rank with ties broken by decision id, not by insertion order and not by value alone: seven decisions with the same
// volatility fall 3/2/2 by id (low = ids 1-3, mid = 4-5, high = 6-7), two without a value are unknown and not ranked. The
// decisions are inserted in reverse id order.
func TestTerciles_TiesAreBrokenByDecisionID(t *testing.T) {
	srv, w := newServer(t)
	scenarios := []string{"up", "down", "mild", "dip", "up", "down", "mild", "up", "up"} // LONG nets +30 -30 0 -20 +30 -30 0 +30 +30
	var specs []spec
	for id := 9; id >= 1; id-- {
		s := spec{id: id, inst: id, at: jst(t, "2026-10-06T10:00:00"), dir: "LONG", scenario: scenarios[id-1], model: m1, version: v3}
		if id <= 7 {
			s.vol = f(5)
		}
		specs = append(specs, s)
	}
	sectors := map[int]string{}
	for id := 1; id <= 9; id++ {
		sectors[id] = ""
	}
	seed(t, w, sectors, specs)
	d := srv.call(t, nil).data(t)
	_, dim := dimOf(t, d.Cohorts[0], "volatility_band")
	for name, want := range map[string]struct {
		n    int
		mean float64
	}{"low": {3, 0}, "mid": {2, 5}, "high": {2, -15}, "unknown": {2, 30}} {
		b := bucketOf(t, dim, name)
		if b.N != want.n || b.MeanNetBps == nil || *b.MeanNetBps != want.mean {
			t.Errorf("%s: n %d mean %v, want %d / %v", name, b.N, deref(b.MeanNetBps), want.n, want.mean)
		}
	}
	if len(dim.Edges) != 2 || dim.Edges[0] != 5 || dim.Edges[1] != 5 {
		t.Errorf("edges = %v, want [5 5]", dim.Edges)
	}
}

// At most 20 sectors keep a bucket: the most rows first, equal counts by name; the rest are other and a row without a
// sector is unknown. The buckets are by name (other and unknown last), and add up to every row.
func TestSectorsAreCappedAtTwenty(t *testing.T) {
	srv, w := newServer(t)
	var specs []spec
	sectors := map[int]string{99: ""}
	id := 0
	add := func(inst, rows int) {
		for range rows {
			id++
			specs = append(specs, spec{id: id, inst: inst, at: slotTime(t, weekdays[id%5], id/5), dir: "LONG", scenario: "up", model: m1, version: v3})
		}
	}
	// sectors 0-4 have 3 rows, 5-24 have 2: the 20 kept are 0-4 and, of the 20 tied at 2 rows, the first 15 by name.
	for i := range 25 {
		sectors[i+1] = sectorName(i)
		add(i+1, map[bool]int{true: 3, false: 2}[i < 5])
	}
	add(99, 1)
	seed(t, w, sectors, specs)
	got := countsOf(t, srv, "sector")
	var want []string
	for i := range 20 {
		want = append(want, fmt.Sprintf("%s:%d", sectorName(i), map[bool]int{true: 3, false: 2}[i < 5]))
	}
	want = append(want, "other:10", "unknown:1")
	equalLines(t, "sector", got, want)
}
