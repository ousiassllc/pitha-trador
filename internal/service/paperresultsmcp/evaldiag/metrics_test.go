package evaldiag

import (
	"math"
	"testing"
)

func ptr(v float64) *float64 { return &v }

// days_to_detect: the minimum scales as 1/sqrt(days), so reaching a target needs
// days * (minimum/target)^2 days in all, and the answer is the ceiling of the
// additional ones.
func TestDaysToDetectArithmetic(t *testing.T) {
	cases := []struct {
		name    string
		days    int
		minimum *float64
		want3   *int
		want5   *int
		now3    bool
		now5    bool
	}{
		// 10 days at a minimum of 6: +3 needs 10*4 = 40 days (30 more); +5 needs 10*1.44 = 14.4 days (ceil 5 more).
		{"above both", 10, ptr(6), ptr0(30), ptr0(5), false, false},
		// 10 days at 4: +3 needs 10*(4/3)^2 = 17.78 days (8 more); 4 <= 5 is already detectable.
		{"between", 10, ptr(4), ptr0(8), ptr0(0), false, true},
		// at or below both targets is already detectable; exactly equal counts as detectable.
		{"already", 10, ptr(3), ptr0(0), ptr0(0), true, true},
		{"zero spread", 10, ptr(0), ptr0(0), ptr0(0), true, true},
		// an exact quotient must not round up on float error: 7 days at 6 needs 28 days in all (21 more); +5 needs 10.08 (ceil 4 more).
		{"exact quotient", 7, ptr(6), ptr0(21), ptr0(4), false, false},
		// no standard error (fewer than two days): undefined, never "0 days" and never "detectable".
		{"undefined", 1, nil, nil, nil, false, false},
	}
	for _, c := range cases {
		got := daysToDetect(c.days, c.minimum)
		if len(got) != 2 || got[0].TargetBps != 3 || got[1].TargetBps != 5 {
			t.Fatalf("%s: targets = %+v", c.name, got)
		}
		for i, want := range []*int{c.want3, c.want5} {
			g := got[i]
			switch {
			case want == nil && g.AdditionalDays != nil, want != nil && (g.AdditionalDays == nil || *g.AdditionalDays != *want):
				t.Errorf("%s: target %v additional days = %v, want %v", c.name, g.TargetBps, deref(g.AdditionalDays), deref(want))
			}
		}
		if got[0].AlreadyDetectable != c.now3 || got[1].AlreadyDetectable != c.now5 {
			t.Errorf("%s: already_detectable = %v/%v, want %v/%v", c.name, got[0].AlreadyDetectable, got[1].AlreadyDetectable, c.now3, c.now5)
		}
	}
}

func ptr0(v int) *int { return &v }

func deref[T any](p *T) any {
	if p == nil {
		return nil
	}
	return *p
}

func rows(days []string, nets ...[]float64) []evalRow {
	var out []evalRow
	for i, day := range days {
		for _, n := range nets[i] {
			r := evalRow{day: day, dir: "LONG"}
			r.net[iLLM] = n
			out = append(out, r)
		}
	}
	return out
}

func llmValue(r evalRow) float64 { return r.net[iLLM] }

// The mean is over rows (not over days), the win rate counts net > 0 only, a
// zero is neither a win nor missing, and the daily list is by JST date.
func TestMetricsOfHandComputed(t *testing.T) {
	rs := rows([]string{"2026-10-06", "2026-10-05", "2026-10-07"}, []float64{10, -30, 0}, []float64{30}, []float64{-10, 0})
	m := metricsOf(rs, llmValue, true)
	if m.N != 6 || m.Days != 3 || math.Abs(*m.MeanNetBps-0) > 1e-12 {
		t.Fatalf("metrics = %+v mean %v", m, deref(m.MeanNetBps))
	}
	if math.Abs(*m.WinRate-2.0/6) > 1e-12 {
		t.Errorf("win rate = %v, want 2/6 (10 and 30 win; the zeros do not)", *m.WinRate)
	}
	if len(m.Daily) != 3 || m.Daily[0].Date != "2026-10-05" || m.Daily[1].Date != "2026-10-06" || m.Daily[1].N != 3 ||
		math.Abs(m.Daily[1].MeanNetBps-(-20.0/3)) > 1e-12 || m.Daily[0].MeanNetBps != 30 {
		t.Errorf("daily = %+v", m.Daily)
	}
	if m.CI == nil || m.CI.Level != 0.95 || m.DetectableMinimumEdgeBps == nil || len(m.DaysToDetect) != 2 {
		t.Errorf("ci %+v minimum %v targets %v", m.CI, deref(m.DetectableMinimumEdgeBps), m.DaysToDetect)
	}
	if metricsOf(rs, llmValue, false).Daily != nil {
		t.Error("no daily list unless asked for")
	}
}

// One day gives a mean but no interval and no detectable minimum (no variance to
// resample); an empty set gives nothing at all but a zero count.
func TestMetricsOfFewDaysAndNoRows(t *testing.T) {
	one := metricsOf(rows([]string{"2026-10-05"}, []float64{5, 15}), llmValue, true)
	if one.N != 2 || *one.MeanNetBps != 10 || one.CI != nil || one.DetectableMinimumEdgeBps != nil {
		t.Errorf("one day = %+v", one)
	}
	for _, d := range one.DaysToDetect {
		if d.AdditionalDays != nil || d.AlreadyDetectable {
			t.Errorf("undefined minimum answered %+v", d)
		}
	}
	none := metricsOf(nil, llmValue, true)
	if none.N != 0 || none.MeanNetBps != nil || none.CI != nil || none.WinRate != nil || none.DaysToDetect == nil {
		t.Errorf("no rows = %+v", none)
	}
}

// The bootstrap is deterministic (fixed seed, fixed resamples): repeated calls
// are identical, and an interval of identical days collapses onto the mean.
func TestBootstrapIsDeterministic(t *testing.T) {
	rs := rows([]string{"2026-10-05", "2026-10-06", "2026-10-07", "2026-10-08"}, []float64{30, -30}, []float64{-30, -30, 0}, []float64{30}, []float64{-20, 10})
	a, b := metricsOf(rs, llmValue, false), metricsOf(rs, llmValue, false)
	if *a.CI != *b.CI || *a.DetectableMinimumEdgeBps != *b.DetectableMinimumEdgeBps {
		t.Fatalf("two runs differ: %+v vs %+v", a.CI, b.CI)
	}
	if a.CI.Lower > *a.MeanNetBps || a.CI.Upper < *a.MeanNetBps || a.CI.Lower >= a.CI.Upper {
		t.Errorf("ci %+v does not bracket the mean %v", a.CI, *a.MeanNetBps)
	}
	flat := metricsOf(rows([]string{"a", "b", "c"}, []float64{5}, []float64{5}, []float64{5}), llmValue, false)
	if flat.CI.Lower != 5 || flat.CI.Upper != 5 || *flat.DetectableMinimumEdgeBps != 0 || !flat.DaysToDetect[0].AlreadyDetectable {
		t.Errorf("flat = %+v", flat)
	}
}

// The paired difference is of the same rows: a day's cluster is the sum of the
// row differences, so the mean difference is the difference of the means.
func TestDiffOfIsPairedOnTheSameRows(t *testing.T) {
	rs := rows([]string{"2026-10-05", "2026-10-06"}, []float64{10, 20}, []float64{-30})
	rs[0].net[iMomentum], rs[1].net[iMomentum], rs[2].net[iMomentum] = 30, 0, -30
	d := diffOf(rs, iLLM, iMomentum) // -20, +20, 0
	if d.N != 3 || d.Days != 2 || math.Abs(*d.MeanDiffBps) > 1e-12 || d.CI == nil || d.DetectableMinimumDifferenceBps == nil {
		t.Fatalf("diff = %+v", d)
	}
	if e := diffOf(nil, iLLM, iMomentum); e.N != 0 || e.MeanDiffBps != nil || e.DaysToDetect == nil {
		t.Errorf("empty diff = %+v", e)
	}
}

func TestSubsetOfSplitsByOwnDirection(t *testing.T) {
	rs := rows([]string{"a", "b"}, []float64{1}, []float64{2})
	rs[1].dir = "SHORT"
	if len(subsetOf(rs, "")) != 2 || len(subsetOf(rs, "LONG")) != 1 || subsetOf(rs, "SHORT")[0].net[iLLM] != 2 {
		t.Error("subsetOf mixes directions")
	}
}
