package replaylayertest

import (
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/replaylayer"
)

// carryScenario: with max_consecutive_losses 3, two losses on day 0 and a third
// on day 1 raise the engine's Kill Switch on day 1 - possible only when the
// loss streak was NOT reset at the day boundary. The engine's own daily reset
// then lifts it on day 2 (the one reset a replay is allowed to make).
//
//	day 0: 09:01 loses, 09:10 loses
//	day 1: 09:02 loses (3rd in a row -> kill), 09:15 would win -> Risk rejects
//	day 2: 09:02 wins (kill lifted by the engine's day reset)
func carryScenario() replaylayer.Input {
	d0 := newScript().loss(1).loss(10)
	d1 := newScript().loss(2).win(15)
	d2 := newScript().win(2)
	bars := d2.bars(d1.bars(d0.bars(nil, 0, 30), 1, 30), 2, 30)
	return input(bars, 3,
		decision(0, 1), decision(0, 10),
		decision(1, 2), decision(1, 15),
		decision(2, 2),
	)
}

func TestContinuousState_CapitalAndLossStreakCarryAcrossDaysExceptTheEnginesOwnReset(t *testing.T) {
	res := run(t, carryScenario())
	if len(res.Segments) != 1 || len(res.Unavailable) != 0 {
		t.Fatalf("segments %+v unavailable %+v, want one continuous segment", res.Segments, res.Unavailable)
	}
	for _, name := range []string{"m_off_q_off", "m_off_q_on", "m_on_q_off", "m_on_q_on"} {
		seg := cell(t, res, name).Segments[0]
		wantPnL := []float64{-40_000, -20_000, 20_000}
		var cum float64
		for i, d := range seg.Daily {
			cum += wantPnL[i]
			if !near(d.PnLJPY, wantPnL[i]) || !near(d.EquityJPY, 10_000_000+cum) {
				t.Errorf("%s: day %d = %+v, want P&L %v and equity %v (carried, not reset daily)", name, i, d, wantPnL[i], 10_000_000+cum)
			}
		}
		if got := seg.DropReasons["risk:risk_engine_rejected:max_consecutive_losses"]; got != 1 {
			t.Errorf("%s: drop reasons %v, want the day-1 09:15 entry rejected by the carried loss streak", name, seg.DropReasons)
		}
		if seg.Counts.Entries != 4 {
			t.Errorf("%s: %d entries, want 4 (day 2's entry proves the engine's own daily reset)", name, seg.Counts.Entries)
		}
		if dd := seg.Drawdown; !near(dd.MaxJPY, 60_000) || dd.PeakDay != "start" || dd.TroughDay != "2026-03-03" {
			t.Errorf("%s: drawdown %+v, want 60,000 yen from the initial capital to 2026-03-03", name, dd)
		}
	}
}

// The reference experiment resets every day, so day 1 no longer sees the streak:
// its 09:15 entry is made. It is labelled, separate from the continuous curve,
// and has no equity or drawdown.
func TestReferenceDailyReset_IsLabelledSeparateAndDiffersFromTheContinuousPath(t *testing.T) {
	in := carryScenario()
	if res := run(t, in); res.ReferenceDailyReset != nil {
		t.Fatal("the reference experiment ran without being asked")
	}
	in.ReferenceDailyReset = true
	res := run(t, in)
	ref := res.ReferenceDailyReset
	if ref == nil || !strings.Contains(ref.Label, "reference") || !strings.Contains(ref.Label, "NOT the continuous") {
		t.Fatalf("reference %+v, want a clearly labelled reference experiment", ref)
	}
	for _, c := range ref.Comparisons {
		if c.Experiment != replaylayer.ExperimentDailyReset {
			t.Errorf("%s: experiment %q, want %q", c.Name, c.Experiment, replaylayer.ExperimentDailyReset)
		}
	}
	for _, c := range res.Comparisons {
		if c.Experiment != replaylayer.ExperimentContinuous {
			t.Errorf("continuous comparison %s labelled %q", c.Name, c.Experiment)
		}
	}
	got := ref.Cells[0]
	if len(got.Days) != 3 || !near(got.Days[1].PnLJPY, 0) {
		t.Fatalf("reference days %+v, want day 1 = -20,000 + 20,000 (09:15 entered: no carried streak)", got.Days)
	}
	if main := cell(t, res, got.Name).Segments[0].Daily[1].PnLJPY; !near(main, -20_000) {
		t.Fatalf("continuous day 1 = %v, want -20,000", main)
	}
}

// gapScenario: day 1 has no market_snapshots at all.
func gapScenario() replaylayer.Input {
	d0 := newScript().win(1).loss(10)
	d2 := newScript().win(2)
	bars := d2.bars(d0.bars(nil, 0, 30), 2, 30)
	in := input(bars, 3, decision(0, 1), decision(0, 10), decision(2, 2))
	// A period that does not start or end at midnight is expanded to whole JST days.
	in.Unavailable = []replaylayer.Unavailable{{From: at(1, 5), To: at(1, 200), Reason: "market_snapshots missing"}}
	return in
}

func TestMissingData_CommonSegmentsAreNotStitchedAndSameDaysForAllCells(t *testing.T) {
	res := run(t, gapScenario())
	if len(res.Unavailable) != 1 || res.Unavailable[0].Reason != "market_snapshots missing" ||
		!res.Unavailable[0].From.Equal(dayMidnight(1)) || !res.Unavailable[0].To.Equal(dayMidnight(2)) {
		t.Fatalf("unavailable %+v, want day 1 listed whole with its reason", res.Unavailable)
	}
	if len(res.Segments) != 2 ||
		!res.Segments[0].To.Equal(dayMidnight(1)) || !res.Segments[1].From.Equal(dayMidnight(2)) ||
		res.Segments[0].InitialCapitalJPY != 10_000_000 || res.Segments[1].InitialCapitalJPY != 10_000_000 {
		t.Fatalf("segments %+v, want two common segments with the same initial capital", res.Segments)
	}
	for _, c := range res.Cells {
		if len(c.Segments) != 2 {
			t.Fatalf("%s: %d segments, want 2", c.Name, len(c.Segments))
		}
		for i, seg := range c.Segments {
			if len(seg.Daily) != len(res.Segments[i].Days) {
				t.Fatalf("%s: segment %d has %d days, want the common %d", c.Name, i, len(seg.Daily), len(res.Segments[i].Days))
			}
			for k, d := range seg.Daily {
				if d.Day != res.Segments[i].Days[k] {
					t.Fatalf("%s: segment %d day %s, want the common %s", c.Name, i, d.Day, res.Segments[i].Days[k])
				}
				if d.Day == "2026-03-03" {
					t.Fatalf("%s: the unavailable day appears in a series", c.Name)
				}
			}
		}
		// Segment 1 restarts from the initial capital: its equity does not continue segment 0's.
		first := c.Segments[1].Daily[0]
		if !near(first.EquityJPY, 10_000_000+first.PnLJPY) {
			t.Errorf("%s: segment 1 equity %v, want the initial capital plus its own P&L %v (never stitched)", c.Name, first.EquityJPY, first.PnLJPY)
		}
	}
	// Comparisons come per segment, with the same days.
	for _, cmp := range res.Comparisons {
		if len(cmp.Segments) != 2 {
			t.Fatalf("%s: %d comparison segments, want 2", cmp.Name, len(cmp.Segments))
		}
	}
}

// A stretch of the window with no bars at all is listed as unavailable (never
// silently dropped), and the replay layer still runs the days that have data.
func TestMissingData_AStretchWithNoBarsIsListedAsUnavailable(t *testing.T) {
	in := gapScenario()
	in.To = dayMidnight(5)
	in.Unavailable = append(in.Unavailable, replaylayer.Unavailable{From: dayMidnight(3), To: dayMidnight(4), Reason: "feed outage"})
	res := run(t, in) // days 0 and 2 have data; the stretch [day 4, day 5) has none
	var found bool
	for _, u := range res.Unavailable {
		if u.Reason == replaylayer.ReasonNoSnapshots && u.From.Equal(dayMidnight(4)) && u.To.Equal(dayMidnight(5)) {
			found = true
		}
	}
	if !found || len(res.Segments) != 2 || len(res.Unavailable) != 3 {
		t.Fatalf("unavailable %+v segments %d, want the empty tail listed with %q", res.Unavailable, len(res.Segments), replaylayer.ReasonNoSnapshots)
	}
}

// A decision stamped inside an unavailable day must not look fresh on the first
// bar after the gap: it is never evaluated and no cell enters from it.
func TestMissingData_ADecisionInsideTheGapIsNeverReplayed(t *testing.T) {
	in := gapScenario()
	gapDecision := decision(1, 100) // would clear every guard and enter
	in.Decisions = append(in.Decisions, gapDecision)
	res := run(t, in)
	base := run(t, gapScenario())
	for i, c := range res.Cells {
		n := c.Totals
		if n.Evaluated > n.DecisionsInWindow || n.NotEvaluated < 0 {
			t.Errorf("%s: counts %+v, want Evaluated <= DecisionsInWindow and NotEvaluated >= 0", c.Name, n)
		}
		if n.Evaluated != base.Cells[i].Totals.Evaluated || n.Entries != base.Cells[i].Totals.Entries {
			t.Errorf("%s: evaluated/entries %d/%d, want %d/%d as without the gap decision", c.Name, n.Evaluated, n.Entries, base.Cells[i].Totals.Evaluated, base.Cells[i].Totals.Entries)
		}
		for _, tr := range c.Segments[1].Trades {
			if tr.DecisionID == gapDecision.ID {
				t.Errorf("%s: traded the decision stamped in the gap", c.Name)
			}
		}
	}
}
