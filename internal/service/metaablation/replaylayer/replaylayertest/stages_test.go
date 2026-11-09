package replaylayertest

import (
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/metaablation"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/replaylayer"
)

// stageScenario has rows failing several stages at once, each on flat prices
// (an entry just times out), spaced so no position is open at the next one.
func stageScenario() replaylayer.Input {
	s := newScript().spread(2, 60).spread(40, 20).spread(60, 20)
	bars := s.bars(nil, 0, 150)
	return input(bars, 1,
		// eligibility (spread 60 > 50) AND entry_quality fair: eligibility is first.
		decision(0, 2, quality("fair")),
		// entry_quality fair AND continuation low AND toxic high: Q on -> quality;
		// Q off -> the first flow guard (continuation).
		decision(0, 20, quality("fair"), continuation(0.1), toxic(0.9)),
		// toxic high AND the model's skip (spread 20): flow guards come before M.
		decision(0, 40, toxic(0.9)),
		// only the model skips it (spread 20): M on -> meta; M off enters.
		decision(0, 60),
		// clears everything: every cell enters.
		decision(0, 80),
		// Jev has no LONG/SHORT direction: not a candidate.
		decision(0, 100, noDirection()),
	)
}

func dropped(c replaylayer.CellResult) map[string]int {
	out := map[string]int{}
	for _, s := range c.Segments[0].Stages {
		out[s.Stage] = s.Dropped
	}
	return out
}

func TestStages_FirstDroppingStageInTheLiveOrderForRowsFailingSeveral(t *testing.T) {
	res := run(t, stageScenario())
	want := map[string]map[string]int{
		"m_off_q_off": {"decision": 1, "eligibility": 1, "quality": 0, "flow_guards": 2, "meta": 0, "risk": 0, "fill": 0},
		"m_off_q_on":  {"decision": 1, "eligibility": 1, "quality": 1, "flow_guards": 1, "meta": 0, "risk": 0, "fill": 0},
		"m_on_q_off":  {"decision": 1, "eligibility": 1, "quality": 0, "flow_guards": 2, "meta": 1, "risk": 0, "fill": 0},
		"m_on_q_on":   {"decision": 1, "eligibility": 1, "quality": 1, "flow_guards": 1, "meta": 1, "risk": 0, "fill": 0},
	}
	entries := map[string]int{"m_off_q_off": 2, "m_off_q_on": 2, "m_on_q_off": 1, "m_on_q_on": 1}
	for name, w := range want {
		c := cell(t, res, name)
		got := dropped(c)
		for stage, n := range w {
			if got[stage] != n {
				t.Errorf("%s: stage %s dropped %d, want %d (all: %v)", name, stage, got[stage], n, got)
			}
		}
		if c.Totals.Evaluated != 6 || c.Totals.Entries != entries[name] {
			t.Errorf("%s: evaluated %d entries %d, want 6 / %d", name, c.Totals.Evaluated, c.Totals.Entries, entries[name])
		}
		// Reached counts fall by exactly what dropped before them.
		stages := c.Segments[0].Stages
		reached := 6
		for _, s := range stages {
			if s.Reached != reached {
				t.Errorf("%s: stage %s reached %d, want %d", name, s.Stage, s.Reached, reached)
			}
			reached -= s.Dropped
		}
		if reached != entries[name] {
			t.Errorf("%s: %d rows left after the last stage, want %d entries", name, reached, entries[name])
		}
	}
}

// The order of the reported stages is the live application order
// (eligibility -> Q -> flow guards -> M -> Risk -> fill), the shared contract's.
func TestStages_OrderIsTheLiveOrder(t *testing.T) {
	res := run(t, stageScenario())
	var names []string
	for _, s := range res.Cells[0].Segments[0].Stages {
		names = append(names, s.Stage)
	}
	want := []string{
		metaablation.StageDecision.String(), metaablation.StageEligibility.String(), metaablation.StageQuality.String(),
		metaablation.StageFlowGuards.String(), metaablation.StageMeta.String(), metaablation.StageRisk.String(), metaablation.StageFill.String(),
	}
	if len(names) != len(want) {
		t.Fatalf("stages %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("stages %v, want %v", names, want)
		}
	}
	order := []metaablation.Stage{
		metaablation.StageEligibility, metaablation.StageQuality, metaablation.StageFlowGuards,
		metaablation.StageMeta, metaablation.StageRisk, metaablation.StageFill,
	}
	for i := 1; i < len(order); i++ {
		if order[i-1] >= order[i] {
			t.Fatal("the shared Stage constants are not in the live order")
		}
	}
}

// Q off lets a row that entry_quality dropped travel on to the later guards,
// so the later stages' counts change (the Q on cell never evaluated them).
func TestStages_QOffRowsReachTheFlowGuards(t *testing.T) {
	res := run(t, stageScenario())
	on, off := cell(t, res, "m_off_q_on").Segments[0].Stages, cell(t, res, "m_off_q_off").Segments[0].Stages
	reached := func(ss []replaylayer.StageCount, stage string) int {
		for _, s := range ss {
			if s.Stage == stage {
				return s.Reached
			}
		}
		return -1
	}
	if reached(on, "flow_guards") != 3 || reached(off, "flow_guards") != 4 {
		t.Fatalf("rows reaching the flow guards: Q on %d, Q off %d, want 3 and 4", reached(on, "flow_guards"), reached(off, "flow_guards"))
	}
}
