package evaldiag

import (
	"math"
	"testing"
)

// Summarize is metricsOf on a bare series: the mean is the plain mean, the interval and
// the standard error need two days, and a series that is constant on every day still has a
// defined mean but no p-value (zero standard error).
func TestSummarize(t *testing.T) {
	pts := []Point{{"2026-11-02", 4}, {"2026-11-02", 2}, {"2026-11-03", 6}, {"2026-11-04", -2}, {"2026-11-05", 5}}
	s := Summarize(pts, true)
	if s.N != 5 || s.Days != 4 || s.MeanNetBps == nil || math.Abs(*s.MeanNetBps-3) > 1e-9 {
		t.Fatalf("summary = %+v", s)
	}
	if s.CI == nil || s.SE == nil || s.P == nil || *s.SE <= 0 || *s.P < 0 || *s.P > 1 {
		t.Errorf("CI/SE/P = %v / %v / %v", s.CI, s.SE, s.P)
	}
	if len(s.Daily) != 4 {
		t.Errorf("daily rows = %d, want 4", len(s.Daily))
	}
	if one := Summarize(pts[:2], false); one.CI != nil || one.SE != nil || one.P != nil || one.MeanNetBps == nil {
		t.Errorf("a single day must have a mean but no interval, SE or p: %+v", one)
	}
	if none := Summarize(nil, false); none.N != 0 || none.MeanNetBps != nil || none.P != nil {
		t.Errorf("empty series: %+v", none)
	}
	same := Summarize([]Point{{"a", 1}, {"b", 1}, {"c", 1}}, false)
	if same.P != nil || same.MeanNetBps == nil {
		t.Errorf("zero-variance series must have no p-value: %+v", same)
	}
	if again := Summarize(pts, false); *again.MeanNetBps != *s.MeanNetBps || *again.CI != *s.CI {
		t.Error("Summarize is not repeatable")
	}
}

// A series totalled per day gives the same summary as the series it was totalled from.
func TestSummarizeDaysMatchesSummarize(t *testing.T) {
	pts := []Point{{"2026-11-02", 4}, {"2026-11-02", 2}, {"2026-11-03", 6}, {"2026-11-04", -2}, {"2026-11-05", 5}, {"2026-11-05", -1}}
	want := Summarize(pts, true)
	got := SummarizeDays([]DayTotals{{"2026-11-05", 2, 4, 1}, {"2026-11-02", 2, 6, 2}, {"2026-11-03", 1, 6, 1}, {"2026-11-04", 1, -2, 0}, {"2026-11-09", 0, 0, 0}})
	if got.N != want.N || got.Days != want.Days || *got.MeanNetBps != *want.MeanNetBps || *got.CI != *want.CI || *got.WinRate != *want.WinRate || *got.P != *want.P {
		t.Errorf("SummarizeDays = %+v, want %+v", got, want)
	}
	if len(got.Daily) != 4 || got.Daily[0].Date != "2026-11-02" {
		t.Errorf("daily rows = %+v", got.Daily)
	}
}
