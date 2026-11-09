package labellayer_test

import (
	"context"
	"errors"
	"math"
	"reflect"
	"slices"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/metaablation"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/labellayer"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/manifest"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/manifest/manifesttest"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/stats"
)

func run(t *testing.T, in labellayer.Input) labellayer.Result {
	t.Helper()
	res, err := labellayer.Run(context.Background(), in)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return res
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

// TestCellsMatchHandCalculation: 3 independent days x 5 common eligible
// candidates = 15; the nets are those of dayNets (the toxic row is eligible but
// taken by no cell).
func TestCellsMatchHandCalculation(t *testing.T) {
	res := run(t, input(t))
	if len(res.Cells) != 4 || !slices.Equal(res.Days, []string{"2026-10-29", "2026-10-30", "2026-10-31"}) {
		t.Fatalf("cells %d days %v", len(res.Cells), res.Days)
	}
	for i, want := range []struct {
		name         string
		takes        int
		sum, perTake float64
	}{
		{"m_off_q_off", 12, -30, -2.5}, {"m_off_q_on", 6, 10, 10.0 / 6}, {"m_on_q_off", 6, 30, 5}, {"m_on_q_on", 3, 25, 25.0 / 3},
	} {
		c := res.Cells[i]
		if c.Name != want.name || c.Takes != want.takes || c.EligibleCandidates != 15 || c.Days != 3 {
			t.Errorf("cell %d = %+v", i, c)
		}
		if !near(c.SumNetBps, want.sum) || !near(c.PerEligibleBps, want.sum/15) || c.PerTakeBps == nil || !near(*c.PerTakeBps, want.perTake) {
			t.Errorf("%s: sum %v per eligible %v per take %v; want %v, %v, %v", c.Name, c.SumNetBps, c.PerEligibleBps, c.PerTakeBps, want.sum, want.sum/15, want.perTake)
		}
	}
}

func TestPairedDifferencesMatchHandCalculation(t *testing.T) {
	res := run(t, input(t))
	cmps := metaablation.Comparisons()
	wantSum := []float64{40, -5, 60, 15, 55, 20}
	wantDiscordant := []int{6, 3, 6, 3, 9, 6}
	if len(res.Comparisons) != len(cmps) || res.PrimaryComparisons != 4 {
		t.Fatalf("%d comparisons, %d primary", len(res.Comparisons), res.PrimaryComparisons)
	}
	for i, c := range res.Comparisons {
		if c.Name != cmps[i].Name || c.From != cmps[i].From.Name() || c.To != cmps[i].To.Name() {
			t.Errorf("comparison %d = %s %s->%s", i, c.Name, c.From, c.To)
		}
		if !near(c.Estimate.Mean, wantSum[i]/15) || c.Counts.Discordant != wantDiscordant[i] || c.Counts.Basis != wantDiscordant[i] ||
			c.Counts.Days != 3 || c.Counts.EligibleCandidates != 15 {
			t.Errorf("%s: mean %v discordant %d counts %+v; want %v, %d", c.Name, c.Estimate.Mean, c.Counts.Discordant, c.Counts, wantSum[i]/15, wantDiscordant[i])
		}
		if c.Reference == cmps[i].Primary {
			t.Errorf("%s: reference flag %v", c.Name, c.Reference)
		}
		if c.Reference {
			// Diagonals are descriptive: no interval, no detectable difference, no verdict.
			if c.Estimate.CI != nil || c.MDE != nil || c.Verdict != "" {
				t.Errorf("%s: reference comparison carries a test: %+v", c.Name, c.Result)
			}
		} else if c.Estimate.CI == nil || !c.Estimate.CI.Contains(c.Estimate.Mean) {
			t.Errorf("%s: CI %+v does not contain the mean %v", c.Name, c.Estimate.CI, c.Estimate.Mean)
		}
	}
	// Takes of the two cells of the first comparison: 12 -> 6.
	if q := res.Comparisons[0].Counts; q.TakesFrom != 12 || q.TakesTo != 6 {
		t.Errorf("takes %+v", q)
	}
}

func TestVerdictsAreUndeterminedWhileMinimumsAreUnset(t *testing.T) {
	res := run(t, input(t))
	for _, c := range res.Comparisons {
		if c.Reference {
			if c.Verdict != "" {
				t.Errorf("%s: diagonal got verdict %q", c.Name, c.Verdict)
			}
			continue
		}
		if c.Verdict != stats.Undetermined || !slices.Contains(c.Reasons, stats.ReasonUnsetPrefix+"min_days") || !slices.Contains(c.Reasons, stats.ReasonUnsetPrefix+"delta") {
			t.Errorf("%s: verdict %q reasons %v", c.Name, c.Verdict, c.Reasons)
		}
	}
}

func TestVerdictBelowRegisteredMinimumsIsUndetermined(t *testing.T) {
	m, h := frozenManifest(t, func(m *manifest.Manifest) {
		d, n, p, delta := 5, 100, 0.8, 1.0
		m.Minimums = manifest.Minimums{MinDays: &d, MinCount: &n, Power: &p, DeltaLabelBps: &delta}
	})
	in := input(t)
	in.Manifest, in.ParamsSHA256 = m, h
	for _, c := range run(t, in).Comparisons {
		if c.Reference {
			continue
		}
		if c.Verdict != stats.Undetermined || !slices.Contains(c.Reasons, stats.ReasonDaysBelowMin) || !slices.Contains(c.Reasons, stats.ReasonCountBelow) {
			t.Errorf("%s: %q %v", c.Name, c.Verdict, c.Reasons)
		}
	}
}

func TestSameInputAndSeedGiveTheSameResult(t *testing.T) {
	a, b := run(t, input(t)), run(t, input(t))
	if !reflect.DeepEqual(a, b) {
		t.Fatal("two runs with the same input and seed differ")
	}
	m, h := frozenManifest(t, func(m *manifest.Manifest) { m.Statistics.Seed++ })
	in := input(t)
	in.Manifest, in.ParamsSHA256 = m, h
	if c := run(t, in); c.Comparisons[0].Estimate.SE == a.Comparisons[0].Estimate.SE && c.Comparisons[2].Estimate.SE == a.Comparisons[2].Estimate.SE {
		t.Error("another seed gave the same bootstrap")
	}
}

func TestOnlyIndependentRowsFeedTheTableAndTheReferenceIsSeparate(t *testing.T) {
	res := run(t, input(t))
	ind := res.Independence
	if ind.Independent != 19 || ind.Reference != 2 || ind.Excluded != 3 {
		t.Fatalf("independence %+v", ind)
	}
	if ind.ByReason["cohort_mismatch"] != 1 || ind.ByReason["in_training_or_calibration_window"] != 1 || ind.ByReason["gap_between_windows"] != 1 {
		t.Errorf("reasons %v", ind.ByReason)
	}
	// The original OOS rows are rescored as reference (population (i)): the
	// 999/12/-8 nets of other rows never enter a cell.
	r := res.Reference
	if r.Rows != 2 || r.Rescored != 2 || r.Takes != 1 || r.MeanNetBpsPerRow == nil || !near(*r.MeanNetBpsPerRow, 6) || !near(*r.MeanNetBpsPerTake, 12) {
		t.Errorf("reference %+v", r)
	}
	for _, c := range res.Cells {
		if c.SumNetBps > 100 || c.SumNetBps < -100 {
			t.Errorf("%s sum %v includes a row it should not", c.Name, c.SumNetBps)
		}
	}
}

func TestFunnelsAndPopulationLimit(t *testing.T) {
	res := run(t, input(t))
	off, on := res.Cells[0].Funnel, res.Cells[3].Funnel
	if off.Unknown != 1 || on.Unknown != 1 || res.PopulationLimit == "" {
		t.Errorf("unknown rows must be identical across cells: %d/%d limit %q", off.Unknown, on.Unknown, res.PopulationLimit)
	}
	// 19 independent rows: 15 candidates + spread + unknown + none + reversion.
	// Q off: eligibility 1 (spread), decision 1, flow guards 3 (toxic), reached 12.
	// Q on: the 6 fair rows drop at quality first.
	if off.ReachedMeta != 12 || on.ReachedMeta != 6 || off.ReversionReachedMeta != 1 || on.ReversionReachedMeta != 1 {
		t.Errorf("reached meta off %d/%d on %d/%d", off.ReachedMeta, off.ReversionReachedMeta, on.ReachedMeta, on.ReversionReachedMeta)
	}
	for _, d := range on.Drops {
		switch d.Stage {
		case "quality":
			if d.Count != 6 {
				t.Errorf("Q on quality drops = %d, want 6", d.Count)
			}
		case "flow_guards":
			if d.Count != 3 {
				t.Errorf("Q on flow guard drops = %d, want 3 (toxic rows)", d.Count)
			}
		}
	}
	for _, d := range off.Drops {
		if d.Stage == "quality" && d.Count != 0 {
			t.Errorf("Q off still drops %d at quality", d.Count)
		}
	}
	if res.Cells[2].MetaSkips != 6 || res.Cells[0].MetaSkips != 0 {
		t.Errorf("meta skips m_on_q_off %d, m_off_q_off %d", res.Cells[2].MetaSkips, res.Cells[0].MetaSkips)
	}
	rv := res.Reversion
	// A SHORT reversion side flips the signed features, so the model skips it.
	if rv.Rows != 1 || rv.ReachedMeta != 1 || rv.ModelSkips != 1 {
		t.Errorf("reversion %+v", rv)
	}
}

func TestMOffAppliesNoMetaFilter(t *testing.T) {
	res := run(t, input(t))
	// The model skips r3 and r4 of every day, M off still takes them: 12 takes,
	// not 6, and the model's skips are not counted under M off.
	if res.Cells[0].Takes != 12 || res.Cells[0].MetaSkips != 0 {
		t.Errorf("M off cell = %+v", res.Cells[0])
	}
}

func TestLabelResultSurvivesDaysWithoutReplayData(t *testing.T) {
	// Run has no input about market-data availability: a day the replay layer
	// cannot run is still in table 1 (all three days above).
	if res := run(t, input(t)); len(res.Days) != 3 || res.Cells[0].Days != 3 {
		t.Fatalf("days %v", res.Days)
	}
}

func TestModelMustBeTheFrozenOne(t *testing.T) {
	in := input(t)
	in.ParamsSHA256 = manifest.ParamsHash("retrained")
	if _, err := labellayer.Run(context.Background(), in); !errors.Is(err, manifest.ErrModelMismatch) {
		t.Errorf("another params hash = %v", err)
	}
	in = input(t)
	other := *in.Model
	other.Version = "lr-other"
	in.Model = &other
	if _, err := labellayer.Run(context.Background(), in); !errors.Is(err, manifest.ErrModelMismatch) {
		t.Errorf("another version = %v", err)
	}
	in = input(t)
	in.Manifest.Rules.MinEVBps = 5
	if _, err := labellayer.Run(context.Background(), in); !errors.Is(err, manifest.ErrTampered) {
		t.Errorf("changed manifest = %v", err)
	}
	in = input(t)
	in.Model = nil
	if _, err := labellayer.Run(context.Background(), in); !errors.Is(err, labellayer.ErrNotRescorable) {
		t.Errorf("no model = %v", err)
	}
}

func TestUnusableModelMakesTheRunNotExecutable(t *testing.T) {
	in := input(t)
	broken := *in.Model
	broken.WinBps = 0
	in.Model = &broken
	if _, err := labellayer.Run(context.Background(), in); !errors.Is(err, labellayer.ErrNotRescorable) {
		t.Fatalf("got %v, want ErrNotRescorable", err)
	}
}

func TestRescoringLeavesTheModelUntouched(t *testing.T) {
	in := input(t)
	before := *in.Model
	before.LR.Weights = slices.Clone(in.Model.LR.Weights)
	run(t, in)
	if !reflect.DeepEqual(before, *in.Model) {
		t.Fatal("the model changed during the re-scoring")
	}
}

func TestCohortIsFrozenInTheManifestAndMismatchedRowsAreExcluded(t *testing.T) {
	in := input(t)
	if got := run(t, in).Independence.ByReason["cohort_mismatch"]; got != 1 {
		t.Fatalf("cohort_mismatch = %d, want the one jev-v1 row", got)
	}
	// A manifest without a cohort cannot be frozen, so the check cannot be skipped.
	m := manifesttest.Valid()
	m.Cohort = ""
	if err := m.Freeze(m.CreatedAt); err == nil {
		t.Fatal("a manifest without a cohort was frozen")
	}
	// Even a hand-built frozen-looking manifest is refused by Run.
	in.Manifest.Cohort = ""
	if _, err := labellayer.Run(context.Background(), in); err == nil {
		t.Error("Run accepted a manifest without a cohort")
	}
	// Another cohort in the manifest changes which rows are independent.
	m2, h := frozenManifest(t, func(m *manifest.Manifest) { m.Cohort = "jev-v1" })
	in = input(t)
	in.Manifest, in.ParamsSHA256 = m2, h
	if res := run(t, in); res.Independence.Independent != 1 {
		t.Errorf("with cohort jev-v1 only the one jev-v1 row is independent, got %+v", res.Independence)
	}
}
