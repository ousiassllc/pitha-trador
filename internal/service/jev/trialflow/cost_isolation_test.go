package trialflow_test

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/jevtrialsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/domain/jevtrial"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/trial"
)

func TestCostSourcesAreDistinguishedAndUnknownIsNeverZero(t *testing.T) {
	e := newEnv(t, metalabelsettings.ModePaper, nil)
	reported := okResp()
	reported.ReportedCostUSD = new(0.0004)
	e.addArm("reported", answer(reported, 3000))
	e.addArm("estimate", answer(okResp(), 3000)) // list price 0.038, 1200 input tokens
	e.addArm("unknown", answer(okResp(), 3000))
	unpriced := defaultArm("unknown")
	unpriced.ListPriceUSDPerMtok = 0
	e.enable(defaultArm("reported"), defaultArm("estimate"), unpriced)
	e.evaluate()

	r := e.rowOf("reported")
	if r.CostSource != jevtrial.CostReported || r.CostUSD == nil || *r.CostUSD != 0.0004 || r.CountedCostUSD != 0.0004 {
		t.Errorf("reported row = %v %v %v", r.CostSource, r.CostUSD, r.CountedCostUSD)
	}
	est := e.rowOf("estimate")
	if want := 1200 * 0.038 / 1e6; est.CostSource != jevtrial.CostListPriceEstimate || est.CostUSD == nil || math.Abs(*est.CostUSD-want) > 1e-12 || math.Abs(est.CountedCostUSD-want) > 1e-12 {
		t.Errorf("estimate row = %v %v %v, want %v", est.CostSource, est.CostUSD, est.CountedCostUSD, want)
	}
	u := e.rowOf("unknown")
	if want := 1200 * jevtrialsettings.WorstCaseUSDPerMtok / 1e6; u.CostSource != jevtrial.CostUnknown || u.CostUSD != nil || math.Abs(u.CountedCostUSD-want) > 1e-12 {
		t.Errorf("unknown row = %v %v %v, want cost NULL and counted %v (worst case, not 0)", u.CostSource, u.CostUSD, u.CountedCostUSD, want)
	}
	if n := e.count(`SELECT COUNT(*) FROM jev_trial_decisions WHERE cost_source = 'unknown' AND cost_usd = 0`); n != 0 {
		t.Errorf("%d unknown rows stored cost_usd = 0", n)
	}
}

type failingInsertResults struct{ trial.Results }

func (failingInsertResults) Insert(context.Context, jevtrial.Decision) (jevtrial.Decision, error) {
	return jevtrial.Decision{}, errors.New("disk full")
}

func TestAnAnsweredCallThatCannotBeStoredCountsAsAFailure(t *testing.T) {
	e := newEnv(t, metalabelsettings.ModePaper, func(r trial.Results) trial.Results { return failingInsertResults{r} })
	e.addArm("a1", answer(okResp(), 3000))
	e.enable(defaultArm("a1"))
	if live := e.evaluate(); live.ID == 0 {
		t.Fatal("the live decision must be unaffected by a failing insert")
	}
	if raw, ok := e.get(jevtrialsettings.FailuresKey("a1")); !ok {
		t.Fatal("no failure counted for the unstorable call")
	} else if f, err := jevtrialsettings.ParseFailures("k", raw); err != nil || f.Count != 1 {
		t.Fatalf("failure counter = %+v (%v)", f, err)
	}
}

func TestTrialRowsNeverEnterTheLiveDecisionRAGOrLearningPaths(t *testing.T) {
	e := newEnv(t, metalabelsettings.ModePaper, nil)
	short := okResp()
	short.Direction = "SHORT"
	e.addArm("a1", answer(short, 3000))
	e.addArm("a2", answer(okResp(), 3000))
	e.enable(defaultArm("a1"), defaultArm("a2"))
	live1 := e.evaluate()
	e.advance(time.Minute) // the RAG excludes decisions at/after the queried state's timestamp
	live2 := e.evaluate()

	if n := e.count(`SELECT COUNT(*) FROM jev_trial_decisions`); n != 4 {
		t.Fatalf("trial rows = %d, want 4", n)
	}
	// Only the live decisions are jev_decisions rows, and only they are indexed for the RAG.
	if n := e.count(`SELECT COUNT(*) FROM jev_decisions`); n != 2 {
		t.Errorf("jev_decisions rows = %d, want 2 (the live ones)", n)
	}
	if n := e.count(`SELECT COUNT(*) FROM jev_decision_vectors`); n != 2 {
		t.Errorf("jev_decision_vectors rows = %d, want 2 (the live decisions only)", n)
	}
	if n := e.count(`SELECT COUNT(*) FROM jev_shadow_decisions`); n != 0 {
		t.Errorf("jev_shadow_decisions rows = %d, want 0: the trial is not the shadow", n)
	}
	if n := e.count(`SELECT COUNT(*) FROM jev_decisions WHERE question_version <> 'trader-v3' OR direction = 'SHORT'`); n != 0 {
		t.Errorf("%d jev_decisions rows carry an arm's answer", n)
	}
	for _, live := range []domain.JevDecision{live1, live2} {
		if live.Direction == nil || *live.Direction != domain.JevDirectionNone {
			t.Errorf("live decision %d direction = %v, want the live model's NONE", live.ID, live.Direction)
		}
	}
	for _, table := range []string{"trade_signals", "calibration_outcomes", "paper_orders", "positions", "jev_shadow_decisions"} {
		if n := e.count(`SELECT COUNT(*) FROM ` + table); n != 0 {
			t.Errorf("%s has %d rows after trial comparisons, want 0", table, n)
		}
	}
	// The second live decision's RAG context holds the first live decision only,
	// never an arm's answer: the arms' request carries it, flagged.
	rows := e.rows()
	for _, d := range rows[2:] {
		if !d.RAGIncludesLiveModelAnswers {
			t.Errorf("arm %s row of the second decision must be flagged: its RAG held the live model's past answer", d.ArmID)
		}
	}
}

func TestSpendReadsTheStoredCountedCostByDayAndArm(t *testing.T) {
	e := newEnv(t, metalabelsettings.ModePaper, nil)
	costly := okResp()
	costly.ReportedCostUSD = new(0.002)
	e.addArm("a1", answer(costly, 3000))
	e.addArm("a2", answer(okResp(), 3000))
	e.enable(defaultArm("a1"), defaultArm("a2"))
	e.evaluate()
	from := e.clock().Add(-time.Hour)
	spend, err := e.results.SpentByArmBetween(context.Background(), "a1", from, e.clock().Add(time.Hour))
	if err != nil || spend.Calls != 1 || spend.CountedCostUSD != 0.002 {
		t.Fatalf("arm spend = %+v (%v)", spend, err)
	}
}
