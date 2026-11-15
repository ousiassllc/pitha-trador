package replayflow_test

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain/jevreplay"
	"github.com/ousiassllc/pitha-trador/internal/domain/jevtrial"
)

// Completion criterion: a mock arm turns a saved input into a decision that is
// recorded in the research table, with the trial row's answer/cost/status
// columns, and nothing is written to the live, shadow or trial tables.
func TestRunner_RecordsADecisionFromASavedInputInTheResearchTable(t *testing.T) {
	e := newEnv(t)
	e.enable()
	e.configure(0.000005, arm("a", "2026-09-01"))
	d1 := e.decide(time.Date(2026, 10, 5, 1, 0, 0, 0, time.UTC), state(1))
	e.decide(time.Date(2026, 10, 6, 1, 0, 0, 0, time.UTC), state(2))
	run, _ := e.start(wholeOctober(10))
	liveBefore, trialBefore, shadowBefore := e.count("jev_decisions"), e.count("jev_trial_decisions"), e.count("jev_shadow_decisions")

	tick(t, e.runner())

	got := e.run(run.ID)
	if got.Status != jevreplay.StatusCompleted || got.CallsDone != 2 || got.CallsFailed != 0 || got.FinishedAt == nil {
		t.Errorf("run = %s done %d failed %d, want completed with 2 calls done", got.Status, got.CallsDone, got.CallsFailed)
	}
	if n := e.count("jev_replay_results"); n != 2 {
		t.Fatalf("jev_replay_results has %d rows, want 2", n)
	}
	var id int64
	if err := e.db.QueryRow(`SELECT id FROM jev_replay_results WHERE jev_decision_id = ?`, d1).Scan(&id); err != nil {
		t.Fatalf("result of decision %d: %v", d1, err)
	}
	res, err := e.store.GetResult(context.Background(), id)
	if err != nil {
		t.Fatalf("GetResult: %v", err)
	}
	if res.RunID != run.ID || res.ArmID != "a" || res.Status != jevtrial.StatusOK || res.Direction == nil || *res.Direction != "LONG" {
		t.Errorf("result = run %d arm %s status %s direction %v", res.RunID, res.ArmID, res.Status, res.Direction)
	}
	if res.CostUSD == nil || *res.CostUSD != 0.000005 || res.CostSource != jevtrial.CostReported || res.CountedCostUSD != 0.000005 {
		t.Errorf("cost = %v source %q counted %v, want the reported 0.000005", res.CostUSD, res.CostSource, res.CountedCostUSD)
	}
	if res.LLMCutoffStatus != jevreplay.CutoffVerified || res.ReferenceOnly || res.RAGIncludesLiveModelAnswers {
		t.Errorf("cutoff %q reference_only %v rag %v, want verified, not reference-only, no RAG", res.LLMCutoffStatus, res.ReferenceOnly, res.RAGIncludesLiveModelAnswers)
	}
	if got.CountedCostUSD != 0.00001 {
		t.Errorf("run counted cost = %v, want 0.00001", got.CountedCostUSD)
	}
	if e.count("jev_decisions") != liveBefore || e.count("jev_trial_decisions") != trialBefore || e.count("jev_shadow_decisions") != shadowBefore {
		t.Error("the replay wrote to jev_decisions, jev_trial_decisions or jev_shadow_decisions")
	}
}

// Completion criterion: the input sent is the saved input as stored (no future data).
func TestRunner_SendsTheSavedInputUnchanged(t *testing.T) {
	e := newEnv(t)
	e.enable()
	e.configure(0.000005, arm("a", ""))
	want := []string{state(1), state(2), state(3)}
	for i, s := range want {
		e.decide(time.Date(2026, 10, 5+i, 1, 0, 0, 0, time.UTC), s)
	}
	e.start(wholeOctober(10))

	tick(t, e.runner())

	got := e.arms["a"].received()
	if len(got) != len(want) {
		t.Fatalf("the arm received %d inputs, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("input %d = %s, want the stored %s byte for byte", i, got[i], want[i])
		}
	}
}

// A decision before the arm's published training cutoff is not targeted; an arm
// without a published cutoff is asked about every decision, marked unverified
// (未確認) and reference-only.
func TestRunner_OnlyTargetsDecisionsAfterAKnownCutoffAndMarksUnknownOnesReferenceOnly(t *testing.T) {
	e := newEnv(t)
	e.enable()
	e.configure(0.000005, arm("known", "2026-10-01"), arm("unknown", ""))
	before := e.decide(time.Date(2026, 10, 1, 23, 59, 59, 0, time.UTC), state(1)) // on the cutoff day: not after it
	after := e.decide(time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC), state(2))
	older := e.decide(time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC), state(3))
	run, plan := e.start(wholeOctober(10))

	if plan.Arms[0].ExcludedBeforeCutoff != 2 || plan.Arms[0].Calls != 1 || plan.Arms[1].Calls != 3 {
		t.Errorf("plan: known arm calls %d excluded %d, unknown arm calls %d; want 1, 2 and 3",
			plan.Arms[0].Calls, plan.Arms[0].ExcludedBeforeCutoff, plan.Arms[1].Calls)
	}
	tick(t, e.runner())

	if got := e.run(run.ID); got.Status != jevreplay.StatusCompleted || got.CallsDone != 4 {
		t.Errorf("run = %s with %d calls done, want completed with 4", got.Status, got.CallsDone)
	}
	asked := func(armID string) map[int64]bool {
		rows, err := e.db.Query(`SELECT jev_decision_id, llm_cutoff_status, reference_only FROM jev_replay_results WHERE arm_id = ?`, armID)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = rows.Close() }()
		out := map[int64]bool{}
		for rows.Next() {
			var (
				id     int64
				status string
				ref    bool
			)
			if err := rows.Scan(&id, &status, &ref); err != nil {
				t.Fatal(err)
			}
			wantStatus, wantRef := jevreplay.CutoffVerified, false
			if armID == "unknown" {
				wantStatus, wantRef = jevreplay.CutoffUnverified, true
			}
			if status != wantStatus || ref != wantRef {
				t.Errorf("arm %s decision %d: cutoff %q reference_only %v, want %q %v", armID, id, status, ref, wantStatus, wantRef)
			}
			out[id] = true
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		return out
	}
	if k := asked("known"); len(k) != 1 || !k[after] || k[before] || k[older] {
		t.Errorf("the known-cutoff arm was asked about %v, want only decision %d (after the cutoff)", k, after)
	}
	if u := asked("unknown"); len(u) != 3 {
		t.Errorf("the unknown-cutoff arm was asked about %v, want all 3", u)
	}
	if jevreplay.CutoffLabel(jevreplay.CutoffUnverified) != "未確認" {
		t.Error("an unverified cutoff is not shown as 未確認")
	}
}

// A run keeps to its max_rows (oldest first) and the estimate token names the
// plan: another arm set or another size is another token.
func TestPlan_RespectsMaxRowsAndTokenFollowsTheContent(t *testing.T) {
	e := newEnv(t)
	e.enable()
	e.configure(0.000005, arm("a", ""), arm("b", ""))
	for i := range 5 {
		e.decide(time.Date(2026, 10, 5+i, 1, 0, 0, 0, time.UTC), state(i))
	}
	_, p2 := e.start(wholeOctober(2))
	_, p2again := e.start(wholeOctober(2))
	_, p3 := e.start(wholeOctober(3))
	_, onlyA := e.start(wholeOctober(2, "a"))

	if len(p2.Decisions) != 2 || len(p2.Calls) != 4 {
		t.Errorf("max_rows 2 over 2 arms: %d decisions %d calls, want 2 and 4", len(p2.Decisions), len(p2.Calls))
	}
	if p2.MaxCostUSD <= 0 || p2.Token == "" || p2.Token != p2again.Token {
		t.Errorf("the same request must give the same token and a worst-case cost: %q vs %q, cost %v", p2.Token, p2again.Token, p2.MaxCostUSD)
	}
	if p2.Token == p3.Token || p2.Token == onlyA.Token {
		t.Error("a different plan has the same token")
	}
	if p2.ListPriceUSD != nil {
		t.Errorf("arms without a list price have a list-price estimate %v", *p2.ListPriceUSD)
	}
}
