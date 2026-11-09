package replaylayertest

import (
	"reflect"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/replaylayer"
)

// handScenario is two days that can be computed by hand (1,000 shares at 1,000
// yen, +20,000 / -20,000 yen per win / loss, 5 minute cooldown after a loss):
//
//	day 0  09:01 strong, spread 10, wins            (every cell enters)
//	       09:10 fair,   spread 10, loses           (Q on: dropped by Q)
//	       09:15 strong, spread 20, would win       (M on: skipped by the model;
//	                                                 (M off, Q off): Risk cooldown after 09:12's loss)
//	day 1  09:02 strong, spread 10, wins            (every cell enters)
func handScenario() replaylayer.Input {
	d0 := newScript().win(1).loss(10).win(15).spread(15, 20)
	d1 := newScript().win(2)
	bars := d1.bars(d0.bars(nil, 0, 30), 1, 30)
	return input(bars, 2,
		decision(0, 1), decision(0, 10, quality("fair")), decision(0, 15),
		decision(1, 2),
	)
}

func TestHandComputedScenario_PerCellCountsMeansAndDailyDifferences(t *testing.T) {
	res := run(t, handScenario())

	// Hand calculation, yen per day, entries, eligible (every decision is eligible).
	want := map[string]struct {
		day0, day1 float64
		entries    int
		bps        float64
	}{
		"m_off_q_off": {0, 20_000, 3, 200}, // +20k, -20k, (09:15 rejected by Risk), +20k: bps +200-200+200
		"m_off_q_on":  {40_000, 20_000, 3, 600},
		"m_on_q_off":  {0, 20_000, 3, 200}, // 09:15 skipped by the model; +20k, -20k, +20k
		"m_on_q_on":   {20_000, 20_000, 2, 400},
	}
	for name, w := range want {
		c := cell(t, res, name)
		daily := c.Segments[0].Daily
		if len(daily) != 2 || !near(daily[0].PnLJPY, w.day0) || !near(daily[1].PnLJPY, w.day1) {
			t.Errorf("%s: daily P&L %+v, want %v / %v", name, daily, w.day0, w.day1)
		}
		if !near(daily[0].EquityJPY, 10_000_000+w.day0) || !near(daily[1].EquityJPY, 10_000_000+w.day0+w.day1) {
			t.Errorf("%s: equity %v / %v, want the P&L carried from day 0 to day 1", name, daily[0].EquityJPY, daily[1].EquityJPY)
		}
		if !near(daily[1].PnLRatio, w.day1/10_000_000) {
			t.Errorf("%s: day 1 ratio %v, want %v of the initial capital", name, daily[1].PnLRatio, w.day1/10_000_000)
		}
		n := c.Totals
		if n.Entries != w.entries || n.Trades != w.entries || n.Eligible != 4 || n.Evaluated != 4 || n.DecisionsInWindow != 4 || n.NotEvaluated != 0 {
			t.Errorf("%s: counts %+v, want %d entries over 4 eligible evaluated decisions", name, n, w.entries)
		}
		if !near(n.NetBpsSum, w.bps) || !near(n.MeanNetBpsPerEntry, w.bps/float64(w.entries)) || !near(n.NetBpsPerEligible, w.bps/4) {
			t.Errorf("%s: bps sum %v mean/entry %v per eligible %v, want sum %v", name, n.NetBpsSum, n.MeanNetBpsPerEntry, n.NetBpsPerEligible, w.bps)
		}
		if !near(n.NetPnLJPY, w.day0+w.day1) || !near(n.PnLJPYPerEligible, (w.day0+w.day1)/4) {
			t.Errorf("%s: P&L %v per eligible %v, want %v", name, n.NetPnLJPY, n.PnLJPYPerEligible, w.day0+w.day1)
		}
	}

	// Daily differences B - A of the four primary comparisons, day 0 / day 1.
	for name, w := range map[string][2]float64{
		"q_off_to_on@m_off": {40_000, 0},
		"q_off_to_on@m_on":  {20_000, 0},
		"m_off_to_on@q_off": {0, 0},
		"m_off_to_on@q_on":  {-20_000, 0},
	} {
		c := comparison(t, res.Comparisons, name)
		if !c.Primary || c.Experiment != replaylayer.ExperimentContinuous {
			t.Errorf("%s: primary=%v experiment=%q", name, c.Primary, c.Experiment)
		}
		d := diffs(t, c, 0, c.BasisStream, replaylayer.UnitJPY)
		if len(d) != 2 || !near(d[0].Diff, w[0]) || !near(d[1].Diff, w[1]) || d[0].Day != "2026-03-02" || d[1].Day != "2026-03-03" {
			t.Errorf("%s: daily diffs %+v, want %v", name, d, w)
		}
		if !near(d[0].B-d[0].A, d[0].Diff) {
			t.Errorf("%s: Diff is not B - A: %+v", name, d[0])
		}
		for _, s := range c.Segments[0].Series {
			if s.Unit == replaylayer.UnitJPY && s.Stream == c.BasisStream && !near(s.Mean, (w[0]+w[1])/2) {
				t.Errorf("%s: mean diff %v, want %v", name, s.Mean, (w[0]+w[1])/2)
			}
		}
		r := diffs(t, c, 0, c.BasisStream, replaylayer.UnitCapitalRatio)
		if !near(r[0].Diff, w[0]/10_000_000) {
			t.Errorf("%s: ratio diff %v, want %v", name, r[0].Diff, w[0]/10_000_000)
		}
	}
}

func TestComparisons_FourPrimaryOneAxisAndDiagonalsAreReferenceOnly(t *testing.T) {
	res := run(t, handScenario())
	if got, want := len(res.Comparisons), len(metaablation.Comparisons()); got != want {
		t.Fatalf("%d comparisons, want %d", got, want)
	}
	primary := 0
	for _, c := range res.Comparisons {
		switch {
		case c.Primary:
			primary++
			if c.Axis != "q" && c.Axis != "m" {
				t.Errorf("%s: primary comparison on axis %q, want exactly one axis", c.Name, c.Axis)
			}
			if (c.Axis == "q") != (c.BasisStream == replaylayer.StreamMomentum) {
				t.Errorf("%s: basis stream %q on axis %q: the Q axis is judged on the momentum stream only", c.Name, c.BasisStream, c.Axis)
			}
		case c.Axis != "both":
			t.Errorf("%s: a non-primary comparison must be a diagonal, axis %q", c.Name, c.Axis)
		}
	}
	if primary != 4 {
		t.Fatalf("%d primary comparisons, want 4", primary)
	}
}

func TestMOffIsNotUnconditional_RiskAndCooldownStillBind(t *testing.T) {
	res := run(t, handScenario())
	off := cell(t, res, "m_off_q_off")
	// The 09:15 candidate cleared every guard and no model scored it, but the
	// Risk Engine's post-loss cooldown (09:12 + 5 min) still rejected it.
	if got := off.Segments[0].DropReasons["risk:risk_engine_rejected:cooldown_after_loss"]; got != 1 {
		t.Fatalf("drop reasons %v, want one Risk cooldown rejection in (M off, Q off)", off.Segments[0].DropReasons)
	}
	if off.Totals.Entries != 3 || off.Totals.Takes != 4 {
		t.Fatalf("counts %+v, want 4 takes (nothing screens at M) and 3 entries", off.Totals)
	}
	// Without that loss the same candidate enters in (M off, Q on).
	on := cell(t, res, "m_off_q_on")
	if on.Segments[0].DropReasons["risk:risk_engine_rejected:cooldown_after_loss"] != 0 || on.Totals.Entries != 3 {
		t.Fatalf("(M off, Q on): %+v %v, want the 09:15 entry", on.Totals, on.Segments[0].DropReasons)
	}
}

func TestOldConfidenceThresholdIsUsedByNoCell(t *testing.T) {
	// guards() has min_probability 0.99 and every decision confidence 0.5.
	res := run(t, handScenario())
	if res.Config.MinProbability != 0 {
		t.Fatalf("recorded min_probability %v, want 0 (unused)", res.Config.MinProbability)
	}
	for _, c := range res.Cells {
		if c.Totals.Entries == 0 {
			t.Errorf("%s: no entry: the old confidence threshold must not gate any cell", c.Name)
		}
	}
}

func TestRun_IsDeterministic(t *testing.T) {
	in := handScenario()
	in.ReferenceDailyReset = true
	a, b := run(t, in), run(t, in)
	if !reflect.DeepEqual(a, b) {
		t.Fatal("two runs on the same input differ")
	}
}

func TestRun_RecordsTheExactConfigurationAndPopulation(t *testing.T) {
	res := run(t, handScenario())
	c := res.Config
	if c.InitialCapitalJPY != 10_000_000 || c.QualityCut != "good" || metaablation.QualityCut != "good" ||
		c.MinEVBps != 2 || c.Risk.Limits.MaxConsecutiveLosses != 3 || c.Exit.TakeProfitPct != 1 ||
		c.MaxSpreadBps != 50 || c.Long.MaxToxicFlow != 0.35 || c.ModelVersion != "lr-test" || c.SegmentPolicy == "" {
		t.Fatalf("config record %+v", c)
	}
	if res.Population == "" {
		t.Fatal("no population description")
	}
	for _, c := range res.Cells {
		if c.Config.ModelApplied != c.Cell.MOn {
			t.Errorf("%s: ModelApplied %v", c.Name, c.Config.ModelApplied)
		}
		wantQ := replaylayer.QualityOffFloor
		if c.Cell.QOn {
			wantQ = domain.JevEntryQualityGood
		}
		if c.Config.MinEntryQuality != wantQ {
			t.Errorf("%s: entry_quality floor %q, want %q", c.Name, c.Config.MinEntryQuality, wantQ)
		}
	}
}
