package rag_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	calrepo "github.com/ousiassllc/pitha-trador/internal/repository/calibration"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/service/rag"
)

// outcomeFixture wires a rag.Service over a fresh DB with the repositories
// the FR-RAG-2/3 outcome-join tests need.
type outcomeFixture struct {
	svc       *rag.Service
	decisions *judgement.DecisionRepository
	outcomes  *calrepo.CalibrationRepository
	instID    int64
	symbol    string
}

func boolPtr(b bool) *bool { return &b }

func newOutcomeFixture(t *testing.T) outcomeFixture {
	t.Helper()
	db := newTestDB(t)
	inst := mustCreateInstrument(t, market.NewInstrumentRepository(db), "9433")
	decisions := judgement.NewDecisionRepository(db)
	return outcomeFixture{
		svc:       rag.NewService(db, decisions, market.NewSnapshotRepository(db)),
		decisions: decisions,
		outcomes:  calrepo.NewCalibrationRepository(db),
		instID:    inst.ID,
		symbol:    inst.Symbol,
	}
}

// addDecision inserts a jev_decisions row and indexes it under in.
// decisionType scout rows carry no direction/confidence/regime.
func (f outcomeFixture) addDecision(t *testing.T, decisionType, direction string, in rag.FeatureInput) domain.JevDecision {
	t.Helper()
	d := domain.JevDecision{
		InstrumentID: f.instID, Symbol: f.symbol, Timestamp: time.Date(2026, 9, 27, 9, 31, 0, 0, time.UTC),
		DecisionType: decisionType, StateHash: "h", StateJSON: `{}`, QuestionVersion: "test",
		ResponseJSON: `{}`, ModelID: "jev-test",
	}
	if decisionType == domain.JevDecisionTypeTrader {
		confidence := 0.8
		d.Direction, d.Confidence = &direction, &confidence
		// jev_decisions stores regime only inside response_json.
		d.ResponseJSON = `{"regime":"TREND"}`
	}
	saved, err := f.decisions.Insert(context.Background(), d)
	if err != nil {
		t.Fatalf("insert decision: %v", err)
	}
	if err := f.svc.IndexDecision(context.Background(), saved.ID, in); err != nil {
		t.Fatalf("IndexDecision: %v", err)
	}
	return saved
}

func (f outcomeFixture) label(t *testing.T, decisionID int64, horizon int, futureReturn float64, correct *bool) {
	t.Helper()
	if _, err := f.outcomes.Insert(context.Background(), domain.CalibrationOutcome{
		JevDecisionID: decisionID, HorizonMinutes: horizon, FutureReturn: futureReturn, WasDirectionCorrect: correct,
	}); err != nil {
		t.Fatalf("insert outcome: %v", err)
	}
}

func TestService_Context_JoinsCalibrationOutcomeUsingShortestHorizon(t *testing.T) {
	f := newOutcomeFixture(t)
	in := rag.FeatureInput{Return1m: ptr(0.01), Return5m: ptr(0.02)}
	d := f.addDecision(t, domain.JevDecisionTypeTrader, domain.JevDirectionLong, in)
	f.label(t, d.ID, 10, -0.5, boolPtr(false))
	f.label(t, d.ID, 5, 0.4, boolPtr(true))

	got, err := f.svc.Context(context.Background(), in, rag.Subject{}, 3)
	if err != nil {
		t.Fatalf("Context: %v", err)
	}
	if len(got.Cases) != 1 {
		t.Fatalf("Cases = %+v, want exactly the one decision", got.Cases)
	}
	c := got.Cases[0]
	if c.Regime == nil || *c.Regime != domain.JevRegimeTrend {
		t.Errorf("Regime = %v, want TREND", c.Regime)
	}
	if c.HorizonMinutes == nil || *c.HorizonMinutes != 5 {
		t.Errorf("HorizonMinutes = %v, want the shortest labeled horizon 5", c.HorizonMinutes)
	}
	if c.FutureReturn == nil || *c.FutureReturn != 0.4 {
		t.Errorf("FutureReturn = %v, want 0.4", c.FutureReturn)
	}
	if c.WasDirectionCorrect == nil || !*c.WasDirectionCorrect {
		t.Errorf("WasDirectionCorrect = %v, want true", c.WasDirectionCorrect)
	}
}

func TestService_Context_NoneDecisionOutcomeHasFutureReturnButNilCorrectness(t *testing.T) {
	f := newOutcomeFixture(t)
	in := rag.FeatureInput{Return1m: ptr(0.01)}
	d := f.addDecision(t, domain.JevDecisionTypeTrader, domain.JevDirectionNone, in)
	f.label(t, d.ID, 5, 0.2, nil)

	got, err := f.svc.Context(context.Background(), in, rag.Subject{}, 1)
	if err != nil {
		t.Fatalf("Context: %v", err)
	}
	c := got.Cases[0]
	if c.FutureReturn == nil || *c.FutureReturn != 0.2 {
		t.Errorf("FutureReturn = %v, want 0.2", c.FutureReturn)
	}
	if c.WasDirectionCorrect != nil {
		t.Errorf("WasDirectionCorrect = %v, want nil for a NONE decision", *c.WasDirectionCorrect)
	}
}

// The labeled decision is the farthest of three candidates, yet it must
// be adopted first (FR-RAG-2), ahead of the closer unlabeled trader and
// scout decisions; unlabeled trader outranks scout.
func TestService_Context_PrefersOutcomeLabeledThenTraderThenScout(t *testing.T) {
	f := newOutcomeFixture(t)
	query := rag.FeatureInput{Return1m: ptr(0.00)}
	f.addDecision(t, domain.JevDecisionTypeScout, "", rag.FeatureInput{Return1m: ptr(0.00)})
	f.addDecision(t, domain.JevDecisionTypeTrader, domain.JevDirectionShort, rag.FeatureInput{Return1m: ptr(0.01)})
	labeled := f.addDecision(t, domain.JevDecisionTypeTrader, domain.JevDirectionLong, rag.FeatureInput{Return1m: ptr(0.05)})
	f.label(t, labeled.ID, 5, 0.3, boolPtr(true))

	got, err := f.svc.Context(context.Background(), query, rag.Subject{}, 3)
	if err != nil {
		t.Fatalf("Context: %v", err)
	}
	if len(got.Cases) != 3 {
		t.Fatalf("Cases = %+v, want 3", got.Cases)
	}
	if got.Cases[0].FutureReturn == nil || got.Cases[0].Direction == nil || *got.Cases[0].Direction != "LONG" {
		t.Errorf("Cases[0] = %+v, want the outcome-labeled LONG decision first", got.Cases[0])
	}
	if got.Cases[1].Direction == nil || *got.Cases[1].Direction != "SHORT" || got.Cases[1].FutureReturn != nil {
		t.Errorf("Cases[1] = %+v, want the unlabeled SHORT trader decision", got.Cases[1])
	}
	if got.Cases[2].Direction != nil {
		t.Errorf("Cases[2] = %+v, want the scout decision last", got.Cases[2])
	}
	if got.Cases[0].Distance <= got.Cases[1].Distance {
		t.Errorf("distances = %v then %v, want the labeled case to be farther (proves reordering)", got.Cases[0].Distance, got.Cases[1].Distance)
	}

	// k=1 keeps only the labeled decision even though two are closer.
	top, err := f.svc.Context(context.Background(), query, rag.Subject{}, 1)
	if err != nil {
		t.Fatalf("Context(k=1): %v", err)
	}
	if len(top.Cases) != 1 || top.Cases[0].FutureReturn == nil {
		t.Errorf("Context(k=1).Cases = %+v, want only the outcome-labeled decision", top.Cases)
	}
}

// Scout decisions are never labelable yet are indexed for every
// candidate every cycle, so they can fill the whole k*4 nearest pool.
// Outcome-labeled trader decisions farther away must still be adopted
// (FR-RAG-2, issues #441/#442), ahead of the closer unlabeled ones.
func TestService_Context_LabeledDecisionsSurviveScoutSaturatedNearestPool(t *testing.T) {
	f := newOutcomeFixture(t)
	const k = 3
	query := rag.FeatureInput{Return1m: ptr(0.00)}
	for range k*4 + 5 {
		f.addDecision(t, domain.JevDecisionTypeScout, "", rag.FeatureInput{Return1m: ptr(0.00)})
	}
	f.addDecision(t, domain.JevDecisionTypeTrader, domain.JevDirectionShort, rag.FeatureInput{Return1m: ptr(0.01)})
	labeledFar := f.addDecision(t, domain.JevDecisionTypeTrader, domain.JevDirectionLong, rag.FeatureInput{Return1m: ptr(0.05)})
	f.label(t, labeledFar.ID, 5, 0.3, boolPtr(true))
	labeledNear := f.addDecision(t, domain.JevDecisionTypeTrader, domain.JevDirectionShort, rag.FeatureInput{Return1m: ptr(0.04)})
	f.label(t, labeledNear.ID, 5, -0.2, boolPtr(false))

	got, err := f.svc.Context(context.Background(), query, rag.Subject{}, k)
	if err != nil {
		t.Fatalf("Context: %v", err)
	}
	if len(got.Cases) != k {
		t.Fatalf("Cases = %+v, want %d", got.Cases, k)
	}
	for i, wantDirection := range []string{"SHORT", "LONG"} {
		c := got.Cases[i]
		if c.FutureReturn == nil || c.Direction == nil || *c.Direction != wantDirection {
			t.Errorf("Cases[%d] = %+v, want the outcome-labeled %s decision (labeled by ascending distance)", i, c, wantDirection)
		}
	}
}

func TestService_Context_UnlabeledDecisionsFallBackToDirectionConfidenceRegime(t *testing.T) {
	f := newOutcomeFixture(t)
	in := rag.FeatureInput{Return1m: ptr(0.01)}
	f.addDecision(t, domain.JevDecisionTypeTrader, domain.JevDirectionLong, in)

	got, err := f.svc.Context(context.Background(), in, rag.Subject{}, 5)
	if err != nil {
		t.Fatalf("Context: %v", err)
	}
	if len(got.Cases) != 1 {
		t.Fatalf("Cases = %+v, want 1", got.Cases)
	}
	c := got.Cases[0]
	if c.Direction == nil || c.Confidence == nil || c.Regime == nil {
		t.Errorf("Direction/Confidence/Regime = %v/%v/%v, want all set", c.Direction, c.Confidence, c.Regime)
	}
	if c.HorizonMinutes != nil || c.FutureReturn != nil || c.WasDirectionCorrect != nil {
		t.Errorf("outcome fields = %v/%v/%v, want all nil without a calibration outcome", c.HorizonMinutes, c.FutureReturn, c.WasDirectionCorrect)
	}
}

func TestSimilarCase_WireJSONCarriesOutcomeFields(t *testing.T) {
	correct := true
	horizon, futureReturn, regime := 5, 0.4, domain.JevRegimeTrend
	b, err := json.Marshal(rag.SimilarCase{
		Source: "jev_decision", Regime: &regime, HorizonMinutes: &horizon, FutureReturn: &futureReturn, WasDirectionCorrect: &correct,
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	for _, want := range []string{`"regime":"TREND"`, `"horizon_minutes":5`, `"future_return":0.4`, `"was_direction_correct":true`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("json = %s, want it to contain %s", b, want)
		}
	}
	unlabeled, _ := json.Marshal(rag.SimilarCase{Source: "jev_decision"})
	for _, absent := range []string{"future_return", "was_direction_correct", "horizon_minutes"} {
		if strings.Contains(string(unlabeled), absent) {
			t.Errorf("unlabeled json = %s, want %s omitted", unlabeled, absent)
		}
	}
}
