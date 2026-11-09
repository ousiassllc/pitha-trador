package eligibility_test

import (
	"slices"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/eligibility"
	"github.com/ousiassllc/pitha-trador/internal/service/policy"
)

func TestFunnelCountsTheFirstRejectingStageOnly(t *testing.T) {
	var rows []eligibility.Result
	add := func(mutate func(*eligibility.Input), n int) {
		for range n {
			in := good(int64(len(rows) + 1))
			mutate(&in)
			rows = append(rows, eval(t, in))
		}
	}
	add(func(*eligibility.Input) {}, 5)                                                 // pass
	add(func(in *eligibility.Input) { in.Policy.SpreadBps = f(50) }, 2)                 // eligibility
	add(func(in *eligibility.Input) { in.Policy.Decision.EntryQuality = s("fair") }, 3) // quality (on) / pass (off)
	add(func(in *eligibility.Input) {
		in.Policy.Decision.EntryQuality = s("poor")
		in.Policy.Decision.ToxicFlow = f(.9)
	}, 4) // quality (on) / flow (off)
	add(func(in *eligibility.Input) { in.Policy.Decision.ToxicFlow = f(.9) }, 1) // flow
	counts := func(fn eligibility.Funnel) map[string]int {
		m := map[string]int{"reached": fn.ReachedMeta}
		for _, d := range fn.Drops {
			m[d.Stage] = d.Count
		}
		return m
	}
	on, off := counts(eligibility.BuildFunnel(rows, true)), counts(eligibility.BuildFunnel(rows, false))
	for stage, want := range map[string]int{"eligibility": 2, "quality": 7, "flow_guards": 1, "reached": 5} {
		if on[stage] != want {
			t.Errorf("Q on %s = %d, want %d", stage, on[stage], want)
		}
	}
	for stage, want := range map[string]int{"eligibility": 2, "quality": 0, "flow_guards": 5, "reached": 8} {
		if off[stage] != want {
			t.Errorf("Q off %s = %d, want %d", stage, off[stage], want)
		}
	}
	total := func(m map[string]int) int {
		return m["reached"] + m["decision"] + m["labeled"] + m["eligibility"] + m["quality"] + m["flow_guards"]
	}
	if total(on) != len(rows) || total(off) != len(rows) {
		t.Errorf("a row was counted twice or not at all: %d/%d of %d", total(on), total(off), len(rows))
	}
}

// TestStageOrderIsThePolicyEnginesOrder removes the failing condition of a row
// that fails every stage one at a time and checks the engine rejects in the
// order eligibility -> quality -> continuation -> toxic -> liquidity, which
// the stage numbering and the funnel follow.
func TestStageOrderIsThePolicyEnginesOrder(t *testing.T) {
	bad := good(1)
	d := bad.Policy.Decision
	d.EntryQuality, d.ContinuationProbability, d.ToxicFlow, d.LiquidityStressed = s("poor"), f(0.1), f(0.9), f(0.9)
	bad.Policy.SpreadBps = f(50)
	steps := []struct {
		fix    func()
		reason string
		stage  metaablation.Stage
	}{
		{func() { bad.Policy.SpreadBps = f(3) }, policy.ReasonSpreadTooWide, metaablation.StageEligibility},
		{func() { d.EntryQuality = s("strong") }, policy.ReasonEntryQualityBelowThreshold, metaablation.StageQuality},
		{func() { d.ContinuationProbability = f(0.9) }, policy.ReasonContinuationProbabilityBelowThreshold, metaablation.StageFlowGuards},
		{func() { d.ToxicFlow = f(0.1) }, policy.ReasonToxicFlowAboveThreshold, metaablation.StageFlowGuards},
		{func() { d.LiquidityStressed = f(0.1) }, policy.ReasonLiquidityStressedAboveThreshold, metaablation.StageFlowGuards},
	}
	prev := metaablation.Stage(0)
	for i, st := range steps {
		r := eval(t, bad)
		if r.QOn.Reason != st.reason || r.QOn.Stage != st.stage {
			t.Fatalf("step %d: Q on = %+v, want %s (%v)", i, r.QOn, st.reason, st.stage)
		}
		if st.stage < prev {
			t.Fatalf("step %d: stage %v precedes %v", i, st.stage, prev)
		}
		prev = st.stage
		st.fix()
	}
	if r := eval(t, bad); r.QOn.Stage != 0 || r.Side != domain.JevDirectionLong {
		t.Fatalf("after every fix: %+v", r)
	}
	ordered := []metaablation.Stage{metaablation.StageEligibility, metaablation.StageQuality, metaablation.StageFlowGuards, metaablation.StageMeta, metaablation.StageRisk}
	if !slices.IsSorted(ordered) || len(slices.Compact(slices.Clone(ordered))) != len(ordered) {
		t.Error("the Stage numbering is not the live order")
	}
}
