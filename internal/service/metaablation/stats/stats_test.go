package stats_test

import (
	"math"
	"slices"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/manifest"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/stats"
)

func ptr[T any](v T) *T { return &v }

// registered is a fully pre-registered plan with arbitrary TEST values.
func registered() stats.Plan {
	return stats.Plan{
		MinDays: ptr(3), MinCount: ptr(2), Delta: ptr(2.0), Power: ptr(0.8),
		Alpha: 0.05, Correction: manifest.CorrectionNone, NComparisons: 1, Resamples: 2000, Seed: 7,
	}
}

func counts(days, basis int) stats.Counts {
	return stats.Counts{Days: days, Basis: basis, BasisName: "discordant_pairs"}
}

func TestMeanIsSumOverUnits(t *testing.T) {
	got, ok := stats.Mean([]stats.Cluster{{Sum: 10, N: 4}, {Sum: -2, N: 2}})
	if !ok || got != 8.0/6 {
		t.Fatalf("Mean = %v, %v; want 8/6", got, ok)
	}
	if _, ok := stats.Mean([]stats.Cluster{{Sum: 0, N: 0}}); ok {
		t.Error("a mean without units is defined")
	}
}

func TestBootstrapIsDeterministicForASeed(t *testing.T) {
	cl := []stats.Cluster{{3, 2}, {-1, 3}, {4, 1}, {0, 2}, {2, 2}}
	a := stats.Bootstrap(cl, 2000, 0.05, 42)
	b := stats.Bootstrap(cl, 2000, 0.05, 42)
	if a.CI == nil || *a.CI != *b.CI || a.SE != b.SE {
		t.Fatalf("same seed, different results: %+v vs %+v", a, b)
	}
	if c := stats.Bootstrap(cl, 2000, 0.05, 43); *c.CI == *a.CI {
		t.Error("another seed gave the identical interval")
	}
	if !a.CI.Contains(a.Mean) {
		t.Errorf("interval %+v does not contain the mean %v", *a.CI, a.Mean)
	}
}

func TestBootstrapNeedsTwoDays(t *testing.T) {
	if est := stats.Bootstrap([]stats.Cluster{{5, 1}}, 2000, 0.05, 1); est.CI != nil || !est.Defined || est.Mean != 5 {
		t.Fatalf("one day = %+v", est)
	}
}

func TestBootstrapOfConstantClustersCollapses(t *testing.T) {
	est := stats.Bootstrap([]stats.Cluster{{2, 1}, {2, 1}, {2, 1}}, 1500, 0.05, 1)
	if est.CI.Lower != 2 || est.CI.Upper != 2 || est.SE != 0 {
		t.Fatalf("constant clusters: %+v", est)
	}
}

func TestVerdictIsUndeterminedWhileAnythingIsUnset(t *testing.T) {
	clear := []stats.DayValue{{"d1", 0}, {"d2", 0}, {"d3", 0}, {"d4", 0}}
	better := []stats.DayValue{{"d1", 10}, {"d2", 11}, {"d3", 10}, {"d4", 12}}
	full := registered()
	res, err := stats.DailyDifference(full, "x", false, clear, better, counts(4, 4))
	if err != nil || res.Verdict != stats.Superior || len(res.Reasons) != 0 {
		t.Fatalf("fully registered clear effect = %+v, %v", res, err)
	}
	for name, mutate := range map[string]func(*stats.Plan){
		"min_days": func(p *stats.Plan) { p.MinDays = nil }, "min_count": func(p *stats.Plan) { p.MinCount = nil },
		"delta": func(p *stats.Plan) { p.Delta = nil }, "power": func(p *stats.Plan) { p.Power = nil },
	} {
		p := registered()
		mutate(&p)
		res, err := stats.DailyDifference(p, "x", false, clear, better, counts(4, 4))
		if err != nil || res.Verdict != stats.Undetermined || !slices.Contains(res.Reasons, stats.ReasonUnsetPrefix+name) {
			t.Errorf("%s unset: %+v, %v", name, res, err)
		}
	}
	if got := (stats.Plan{}).UnsetFields(); len(got) != 4 {
		t.Errorf("zero plan unset = %v", got)
	}
}

func TestInferiorWhenTheIntervalIsBelowZero(t *testing.T) {
	from := []stats.DayValue{{"d1", 10}, {"d2", 11}, {"d3", 10}}
	to := []stats.DayValue{{"d1", 0}, {"d2", 1}, {"d3", 0}}
	res, err := stats.DailyDifference(registered(), "x", false, from, to, counts(3, 3))
	if err != nil || res.Verdict != stats.Inferior || res.Estimate.Mean != -10 {
		t.Fatalf("got %+v, %v", res, err)
	}
}

func TestUndeterminedCases(t *testing.T) {
	noisyFrom := []stats.DayValue{{"d1", 0}, {"d2", 0}, {"d3", 0}, {"d4", 0}}
	noisyTo := []stats.DayValue{{"d1", 5}, {"d2", -5}, {"d3", 4}, {"d4", -4}}
	res, _ := stats.DailyDifference(registered(), "x", false, noisyFrom, noisyTo, counts(4, 4))
	if res.Verdict != stats.Undetermined || !slices.Contains(res.Reasons, stats.ReasonCIContains0) {
		t.Errorf("interval holding 0: %+v", res)
	}
	from := []stats.DayValue{{"d1", 0}, {"d2", 0}, {"d3", 0}}
	to := []stats.DayValue{{"d1", 10}, {"d2", 10}, {"d3", 10}}
	short, _ := stats.DailyDifference(registered(), "x", false, from, to, counts(3, 1))
	if short.Verdict != stats.Undetermined || !slices.Contains(short.Reasons, stats.ReasonCountBelow) {
		t.Errorf("count below min: %+v", short)
	}
	p := registered()
	p.MinDays = ptr(4)
	fewDays, _ := stats.DailyDifference(p, "x", false, from, to, counts(3, 3))
	if fewDays.Verdict != stats.Undetermined || !slices.Contains(fewDays.Reasons, stats.ReasonDaysBelowMin) {
		t.Errorf("days below min: %+v", fewDays)
	}
	one, _ := stats.DailyDifference(registered(), "x", false, from[:1], to[:1], counts(1, 3))
	if one.Verdict != stats.Undetermined || !slices.Contains(one.Reasons, stats.ReasonNoInterval) {
		t.Errorf("one day: %+v", one)
	}
}

func TestDetectableDifferenceAboveDeltaIsUndetermined(t *testing.T) {
	// A clearly positive but noisy series: the interval excludes 0, yet the
	// smallest difference the observed variance can detect is far above delta.
	from := []stats.DayValue{{"d1", 0}, {"d2", 0}, {"d3", 0}, {"d4", 0}, {"d5", 0}, {"d6", 0}}
	to := []stats.DayValue{{"d1", 100}, {"d2", 101}, {"d3", 99}, {"d4", 100}, {"d5", 102}, {"d6", 98}}
	p := registered()
	p.Delta = ptr(0.01)
	res, _ := stats.DailyDifference(p, "x", false, from, to, counts(6, 6))
	if res.MDE == nil || *res.MDE <= 0.01 || res.Verdict != stats.Undetermined || !slices.Contains(res.Reasons, stats.ReasonMDEAboveDelta) {
		t.Fatalf("got %+v", res)
	}
}

func TestMinimumDetectableDifferenceFormula(t *testing.T) {
	// SE is the bootstrap SD; the MDE is (z_{1-a/2} + z_power) * SE, from the
	// variance only (never the observed difference).
	from := []stats.DayValue{{"d1", 0}, {"d2", 0}, {"d3", 0}, {"d4", 0}}
	to := []stats.DayValue{{"d1", 1}, {"d2", 3}, {"d3", -1}, {"d4", 5}}
	p := registered()
	res, _ := stats.DailyDifference(p, "x", false, from, to, counts(4, 4))
	want := (1.959964 + 0.841621) * res.Estimate.SE
	if res.MDE == nil || math.Abs(*res.MDE-want) > 1e-3*want {
		t.Fatalf("MDE = %v, want %v", res.MDE, want)
	}
	shifted := make([]stats.DayValue, len(to))
	for i, d := range to {
		shifted[i] = stats.DayValue{Day: d.Day, Value: d.Value + 1000}
	}
	res2, _ := stats.DailyDifference(p, "x", false, from, shifted, counts(4, 4))
	if math.Abs(*res2.MDE-*res.MDE) > 1e-6 {
		t.Errorf("the MDE moved with the observed difference: %v vs %v", *res2.MDE, *res.MDE)
	}
	p.Power = nil
	if res3, _ := stats.DailyDifference(p, "x", false, from, to, counts(4, 4)); res3.MDE != nil {
		t.Error("an MDE without a pre-registered power")
	}
}

func TestBonferroniAdjustsTheLevelAndRecordsTheCount(t *testing.T) {
	p := registered()
	p.Correction, p.NComparisons = manifest.CorrectionBonferroni, 8
	if got := p.AlphaAdjusted(); got != 0.05/8 {
		t.Fatalf("adjusted alpha = %v", got)
	}
	from := []stats.DayValue{{"d1", 0}, {"d2", 0}}
	res, _ := stats.DailyDifference(p, "x", false, from, from, counts(2, 0))
	if res.Correction != manifest.CorrectionBonferroni || res.NComparisons != 8 || res.AlphaAdjusted != 0.05/8 || res.Estimate.Level != 1-0.05/8 {
		t.Fatalf("not recorded: %+v", res)
	}
	if err := stats.CheckFamily(p, 8); err != nil {
		t.Errorf("8 of 8: %v", err)
	}
	if err := stats.CheckFamily(p, 9); err == nil {
		t.Error("9 comparisons over a pre-registered 8 passed")
	}
}

func TestReferenceComparisonHasNoVerdict(t *testing.T) {
	from := []stats.DayValue{{"d1", 0}, {"d2", 0}, {"d3", 0}}
	to := []stats.DayValue{{"d1", 10}, {"d2", 11}, {"d3", 10}}
	res, _ := stats.DailyDifference(registered(), "diag", true, from, to, counts(3, 3))
	if res.Verdict != "" || !res.Reference || res.Estimate.Mean < 10 {
		t.Fatalf("reference result = %+v", res)
	}
}

func TestDailyDifferenceChecksAlignment(t *testing.T) {
	a := []stats.DayValue{{"d1", 1}, {"d2", 2}}
	if _, err := stats.DailyDifference(registered(), "x", false, a, a[:1], counts(2, 2)); err == nil {
		t.Error("series of different length accepted")
	}
	if _, err := stats.DailyDifference(registered(), "x", false, a, []stats.DayValue{{"d1", 1}, {"d3", 2}}, counts(2, 2)); err == nil {
		t.Error("misaligned days accepted")
	}
}

func TestDailyDifferenceMatchesHandCalculation(t *testing.T) {
	from := []stats.DayValue{{"d1", 100}, {"d2", -50}, {"d3", 0}}
	to := []stats.DayValue{{"d1", 130}, {"d2", -20}, {"d3", 60}}
	res, err := stats.DailyDifference(registered(), "x", false, from, to, counts(0, 3))
	if err != nil || res.Estimate.Mean != 40 || res.Counts.Days != 3 {
		t.Fatalf("mean = %v days = %d (%v); want 40, 3", res.Estimate.Mean, res.Counts.Days, err)
	}
	again, _ := stats.DailyDifference(registered(), "x", false, from, to, counts(0, 3))
	if *again.Estimate.CI != *res.Estimate.CI {
		t.Error("same input and seed gave another interval")
	}
}

func TestPlanFromRequiresAFrozenManifest(t *testing.T) {
	if _, err := stats.PlanFrom(manifest.Manifest{}, manifest.LayerLabel); err == nil {
		t.Error("an unfrozen manifest gave a plan")
	}
}
