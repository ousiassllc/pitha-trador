package evaltests

import (
	"math"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/evaldiag"
)

// Every trader LONG/SHORT decision of the period is counted whatever happened to
// it (a signal that passed, rejected signals, no signal), NONE decisions, scouts
// and decisions outside the period are not, and the LLM headline equals the mean
// of the net_return_bps the production Labeler stored for the same rows.
func TestEveryTraderDecisionIsEvaluatedAndMatchesTheStoredLabel(t *testing.T) {
	srv, w := newServer(t)
	seedSmall(t, w)
	d := srv.call(t, nil).data(t)

	if d.Decisions != 14 { // ids 1-14; the NONE (15), the scout (16) and the decision before the period (17) are not counted
		t.Fatalf("decisions = %d, want 14 (every trader LONG/SHORT decision in the period)", d.Decisions)
	}
	if len(d.Cohorts) != 2 {
		t.Fatalf("cohorts = %+v, want model-1/trader-v3 and model-2/trader-v4", d.Cohorts)
	}
	if d.HorizonMinutes != 10 || d.LabelAssumptions.TakeProfitBps != 30 || d.LabelAssumptions.StopLossBps != 30 ||
		d.LabelAssumptions.LabelCostBps.Long != 10 || d.LabelAssumptions.LabelCostBps.Short != 10 {
		t.Errorf("label assumptions = %+v, horizon %d", d.LabelAssumptions, d.HorizonMinutes)
	}
	if d.Notice == "" || len(d.Definitions) == 0 || len(d.Caveats) == 0 {
		t.Error("notice, definitions and caveats are always returned")
	}

	a := cohortOf(t, d, m1, v3)
	if a.Decisions != 11 || a.Evaluated != 6 || a.ExcludedBeforeCutoff != 0 {
		t.Fatalf("cohort A = decisions %d evaluated %d before cutoff %d, want 11/6/0", a.Decisions, a.Evaluated, a.ExcludedBeforeCutoff)
	}
	if a.FirstDecisionDay != "2026-10-05" || a.LastDecisionDay != "2026-10-07" {
		t.Errorf("cohort A days = %s..%s", a.FirstDecisionDay, a.LastDecisionDay)
	}
	sum := a.ExcludedBeforeCutoff + a.Evaluated
	for _, b := range a.ExcludedByReason {
		sum += b.Count
	}
	if sum != a.Decisions {
		t.Errorf("cohort A: excluded + evaluated = %d, decisions %d", sum, a.Decisions)
	}
	for reason, want := range map[string]int{
		evaldiag.PrefixLLM + evaldiag.KindNoEntry: 1, evaldiag.PrefixLLM + evaldiag.KindWindowShort: 1,
		evaldiag.PrefixOpposite + evaldiag.KindNoEntry: 1, evaldiag.PrefixOpposite + evaldiag.KindWindowShort: 1,
		evaldiag.ReasonNoBarMomentum: 1,
	} {
		if got := reasonCount(a, reason); got != want {
			t.Errorf("excluded %s = %d, want %d", reason, got, want)
		}
	}

	// LLM headline: (+30 -30 -30 +30 0 -30) / 6 = -5; LONG rows (1,3,5) 0; SHORT rows (2,4,6) -10.
	llm := armOf(t, a, evaldiag.ArmLLM)
	wantMean(t, "llm all", llm.All, 6, -5)
	wantMean(t, "llm long", llm.Long, 3, 0)
	wantMean(t, "llm short", llm.Short, 3, -10)
	if llm.All.Days != 3 || llm.All.WinRate == nil || math.Abs(*llm.All.WinRate-2.0/6) > eps {
		t.Errorf("llm all days=%d win rate=%v, want 3 days and 2/6", llm.All.Days, deref(llm.All.WinRate))
	}
	// Daily means: 10-05 (+30,-30) 0; 10-06 (-30,+30) 0; 10-07 (0,-30) -15.
	wantDaily := []struct {
		date string
		n    int
		mean float64
	}{{"2026-10-05", 2, 0}, {"2026-10-06", 2, 0}, {"2026-10-07", 2, -15}}
	if len(llm.All.Daily) != 3 {
		t.Fatalf("daily = %+v", llm.All.Daily)
	}
	for i, w := range wantDaily {
		if g := llm.All.Daily[i]; g.Date != w.date || g.N != w.n || math.Abs(g.MeanNetBps-w.mean) > eps {
			t.Errorf("daily[%d] = %+v, want %+v", i, g, w)
		}
	}
	if ci := llm.All.CI; ci == nil || ci.Level != 0.95 || ci.Lower > -5 || ci.Upper < -5 {
		t.Errorf("llm ci = %+v, want a 95%% interval around -5", ci)
	}

	// The stored label (written by the Labeler) is the same number on the same rows.
	s := a.StoredLabel
	if s.LabelVersion != 2 || s.HorizonMinutes != 10 {
		t.Errorf("stored label = %+v", s)
	}
	// All 11 decisions: labels exist for 1-6, 11, 12, 13 (9 and 10 have no entry / short window): (30-30-30+30+0-30 -30 +0 +30)/9.
	if s.AllDecisions.Decisions != 11 || s.AllDecisions.WithLabel != 9 || s.AllDecisions.WithoutLabel != 2 ||
		s.AllDecisions.MeanNetBps == nil || math.Abs(*s.AllDecisions.MeanNetBps-(-30.0/9)) > eps {
		t.Errorf("stored all = %+v mean %v", s.AllDecisions, deref(s.AllDecisions.MeanNetBps))
	}
	if s.EvaluatedRows.WithLabel != 6 || s.EvaluatedRows.MeanNetBps == nil || math.Abs(*s.EvaluatedRows.MeanNetBps-(*llm.All.MeanNetBps)) > eps {
		t.Errorf("stored on the evaluated rows = %+v mean %v, llm headline %v", s.EvaluatedRows, deref(s.EvaluatedRows.MeanNetBps), deref(llm.All.MeanNetBps))
	}
	if s.MaxAbsDifferenceBps == nil || *s.MaxAbsDifferenceBps > eps || s.RecomputedMeanOnLabeled == nil || math.Abs(*s.RecomputedMeanOnLabeled+5) > eps {
		t.Errorf("recomputed vs stored: %v / %v", deref(s.RecomputedMeanOnLabeled), deref(s.MaxAbsDifferenceBps))
	}

}

// The baselines are scored on the very same six rows with the same label; the
// hand-computed values are in small()'s table.
func TestBaselinesAreScoredOnTheSameRowsByDefinition(t *testing.T) {
	srv, w := newServer(t)
	seedSmall(t, w)
	a := cohortOf(t, srv.call(t, nil).data(t), m1, v3)

	// Per row (ids 1-6): long leg 30 30 -30 -30 0 30, short leg -30 -30 30 30 -20 -30,
	// random (their mean) 0 0 0 0 -10 0, momentum 30 30 30 30 0 0, contrarian -30 -30 -30 -30 0 0.
	// LONG rows are ids 1, 3, 5 and SHORT rows 2, 4, 6.
	check := func(arm string, all, long, short float64) {
		t.Helper()
		got := armOf(t, a, arm)
		wantMean(t, arm+" all", got.All, 6, all)
		wantMean(t, arm+" long", got.Long, 3, long)
		wantMean(t, arm+" short", got.Short, 3, short)
	}
	check(evaldiag.ArmRandom, -5.0/3, -10.0/3, 0)
	check(evaldiag.ArmMomentum, 20, 20, 20)
	check(evaldiag.ArmContrarian, -20, -20, -20)
	check(evaldiag.ArmNone, 0, 0, 0)
	check(evaldiag.ArmLongLeg, 5, 0, 10)
	check(evaldiag.ArmShortLeg, -50.0/6, -20.0/3, -10)

	// random is exactly the mean of the two legs, and none never wins or loses.
	long, short, random := armOf(t, a, evaldiag.ArmLongLeg), armOf(t, a, evaldiag.ArmShortLeg), armOf(t, a, evaldiag.ArmRandom)
	if math.Abs(*random.All.MeanNetBps-(*long.All.MeanNetBps+*short.All.MeanNetBps)/2) > eps {
		t.Errorf("random %v != mean of the legs %v / %v", *random.All.MeanNetBps, *long.All.MeanNetBps, *short.All.MeanNetBps)
	}
	none := armOf(t, a, evaldiag.ArmNone)
	if none.All.WinRate == nil || *none.All.WinRate != 0 || none.All.CI == nil || none.All.CI.Lower != 0 || none.All.CI.Upper != 0 {
		t.Errorf("none = %+v", none.All)
	}
	if got := armOf(t, a, evaldiag.ArmMomentum).All.WinRate; got == nil || math.Abs(*got-4.0/6) > eps {
		t.Errorf("momentum win rate = %v, want 4/6", deref(got))
	}

	// llm minus baseline, row by row: llm 30 -30 -30 30 0 -30 (mean -5).
	for baseline, want := range map[string]float64{
		evaldiag.ArmRandom: -5 + 5.0/3, evaldiag.ArmMomentum: -25, evaldiag.ArmContrarian: 15,
		evaldiag.ArmNone: -5, evaldiag.ArmLongLeg: -10, evaldiag.ArmShortLeg: -5 + 50.0/6,
	} {
		df := diffOf(t, a, baseline)
		if df.All.N != 6 || df.All.MeanDiffBps == nil || math.Abs(*df.All.MeanDiffBps-want) > eps {
			t.Errorf("llm - %s = %v (n %d), want %v", baseline, deref(df.All.MeanDiffBps), df.All.N, want)
		}
		if df.Long.N != 3 || df.Short.N != 3 || df.All.CI == nil || df.All.DetectableMinimumDifferenceBps == nil {
			t.Errorf("llm - %s: %+v", baseline, df)
		}
	}
	// llm - none is the llm's own mean, so it has the llm's own interval.
	none2, llm := diffOf(t, a, evaldiag.ArmNone), armOf(t, a, evaldiag.ArmLLM)
	if *none2.All.CI != *llm.All.CI {
		t.Errorf("llm - none ci %+v != llm ci %+v", *none2.All.CI, *llm.All.CI)
	}

	// Entered (a signal passed Risk: ids 1, 4) vs skipped (2, 3, 5, 6): llm +30 +30 / -30 -30 0 -30.
	if len(a.LLMByEntry) != 2 || a.LLMByEntry[0].Entry != evaldiag.Entered || a.LLMByEntry[1].Entry != evaldiag.Skipped {
		t.Fatalf("llm_by_entry = %+v", a.LLMByEntry)
	}
	wantMean(t, "entered", a.LLMByEntry[0].All, 2, 30)
	wantMean(t, "skipped", a.LLMByEntry[1].All, 4, -22.5)
	if len(a.LLMByEntry[0].All.Daily) != 0 {
		t.Error("the entry split carries no daily list")
	}
}

// Each (model_id, question_version) is its own block with its own counts, and the
// prompt_version / model_id arguments narrow the period to one of them.
func TestCohortsSplitByModelAndPromptVersion(t *testing.T) {
	srv, w := newServer(t)
	seedSmall(t, w)
	d := srv.call(t, nil).data(t)
	if d.Cohorts[0].ModelID != m1 || d.Cohorts[1].ModelID != m2 {
		t.Fatalf("cohorts are ordered by first decision: %+v", []string{d.Cohorts[0].ModelID, d.Cohorts[1].ModelID})
	}
	b := cohortOf(t, d, m2, v4)
	if b.Decisions != 3 || b.Evaluated != 3 || b.FirstDecisionDay != "2026-10-07" || b.LastDecisionDay != "2026-10-08" {
		t.Fatalf("cohort B = %+v", b)
	}
	// ids 7 (+30), 8 (-20), 14 (+30, on instrument 2: its own bars say SHORT wins even
	// though instrument 1's bars at that minute say the opposite).
	wantMean(t, "B llm", armOf(t, b, evaldiag.ArmLLM).All, 3, 40.0/3)
	wantMean(t, "B momentum", armOf(t, b, evaldiag.ArmMomentum).All, 3, 0)           // -30 (id 7: short), 0 (id 8: long leg), +30 (id 14: short)
	wantMean(t, "B contrarian", armOf(t, b, evaldiag.ArmContrarian).All, 3, -20.0/3) // +30 (id 7: long), -20 (id 8: short), -30 (id 14: long)
	if b.StoredLabel.AllDecisions.WithLabel != 3 || *b.StoredLabel.AllDecisions.MeanNetBps-40.0/3 > eps {
		t.Errorf("B stored = %+v", b.StoredLabel)
	}

	for args, wantCohorts := range map[string]int{"prompt_version": 1, "model_id": 1} {
		value := map[string]string{"prompt_version": v4, "model_id": m2}[args]
		only := srv.call(t, map[string]any{args: value}).data(t)
		if len(only.Cohorts) != 1 || only.Cohorts[0].ModelID != m2 || only.Decisions != 3 || len(only.Cohorts) != wantCohorts {
			t.Errorf("%s=%s: %d cohorts, %d decisions", args, value, len(only.Cohorts), only.Decisions)
		}
	}
	none := srv.call(t, map[string]any{"prompt_version": "trader-v0"}).data(t)
	if none.Decisions != 0 || len(none.Cohorts) != 0 {
		t.Errorf("an unknown version has no cohort: %+v", none)
	}
}
