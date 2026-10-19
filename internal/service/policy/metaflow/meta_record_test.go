package metaflow_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	repo "github.com/ousiassllc/pitha-trador/internal/repository/metalabel"
	"github.com/ousiassllc/pitha-trador/internal/service/metalabel"
	"github.com/ousiassllc/pitha-trador/internal/service/policy"
	"github.com/ousiassllc/pitha-trador/internal/service/policy/policytest"
)

func TestEvaluate_RecordsEveryScoredDecisionsPrediction(t *testing.T) {
	env := newMetaEnv(t, domain.JevDirectionLong)
	newEngine := env.newEngine
	preds := &policytest.Predictions{}
	e := newEngine(policytest.Option(policytest.ModelOf(policytest.TakingModel()), preds))

	if _, err := e.Evaluate(context.Background(), env.in); err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	got := preds.Recorded()
	if len(got) != 1 {
		t.Fatalf("predictions = %+v, want one", got)
	}
	p := got[0]
	if p.ModelID != 11 || p.JevDecisionID != env.in.Decision.ID || p.Action != repo.ActionTake || p.SizeFraction <= 0 || p.SizeFraction > 1 ||
		p.RawProbability < 0.87 || p.CalibratedProbability < 0.87 || p.ExpectedValueBps < 23 || p.ExpectedValueBps > 25 || p.PredictedAt.IsZero() {
		t.Errorf("take prediction = %+v", p)
	}
	if !strings.Contains(p.FeaturesJSON, `"ofi_short":1`) || !strings.Contains(p.FeaturesJSON, `"tdnet_score":0`) {
		t.Errorf("features_json = %s, want the raw features scored", p.FeaturesJSON)
	}

	// A skip is recorded as well, with its reason.
	preds = &policytest.Predictions{}
	if _, err := newEngine(policytest.Option(policytest.ModelOf(policytest.SkippingModel()), preds)).Evaluate(context.Background(), env.in); err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	got = preds.Recorded()
	if len(got) != 1 || got[0].Action != repo.ActionSkip || got[0].SizeFraction != 0 || got[0].Reason != metalabel.ReasonEVBelowMin || got[0].ExpectedValueBps >= 0 {
		t.Fatalf("skip prediction = %+v", got)
	}

	// A decision that fails an earlier gate never reaches the model.
	preds = &policytest.Predictions{}
	gated := env.in
	gated.SpreadBps = ptr(999.0)
	if _, err := newEngine(policytest.Option(policytest.ModelOf(policytest.TakingModel()), preds)).Evaluate(context.Background(), gated); err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if got := preds.Recorded(); len(got) != 0 {
		t.Errorf("predictions = %+v, want none for a candidate rejected before the model", got)
	}
}

func TestEvaluate_PersistsThePredictionThroughTheRealStore(t *testing.T) {
	env := newMetaEnv(t, domain.JevDirectionLong)
	store := policytest.NewStore(t, env.db, policytest.TakingModel())

	sig, err := env.newEngine(policytest.Option(store, store)).Evaluate(context.Background(), env.in)
	if err != nil || sig.Direction != domain.JevDirectionLong {
		t.Fatalf("Evaluate = %+v, %v, want LONG", sig, err)
	}
	model, found, err := store.LatestActiveModel(context.Background())
	if err != nil || !found {
		t.Fatalf("LatestActiveModel = %v, %v", found, err)
	}
	p, found, err := store.PredictionForDecision(context.Background(), model.ID, env.in.Decision.ID)
	if err != nil || !found || p.Action != repo.ActionTake || p.CalibratedProbability < 0.87 {
		t.Fatalf("stored prediction = %+v, found %v, err %v", p, found, err)
	}
}

func TestEvaluate_PredictionStoreFailureFailsWithoutPersistingTheSignal(t *testing.T) {
	env := newMetaEnv(t, domain.JevDirectionLong)
	newEngine, signals := env.newEngine, env.signals
	boom := errors.New("disk full")
	e := newEngine(policytest.Option(policytest.ModelOf(policytest.TakingModel()), &policytest.Predictions{Err: boom}))

	if _, err := e.Evaluate(context.Background(), env.in); !errors.Is(err, boom) {
		t.Fatalf("Evaluate error = %v, want the prediction store's error", err)
	}
	if got, err := signals.ListByInstrument(context.Background(), env.in.InstrumentID, 10); err != nil || len(got) != 0 {
		t.Errorf("persisted signals = %v (err %v), want none: an entry without its stored prediction is not allowed", got, err)
	}
}

func TestEvaluate_MissingFeatureIsASkip(t *testing.T) {
	env := newMetaEnv(t, domain.JevDirectionLong)
	newEngine := env.newEngine
	preds := &policytest.Predictions{}
	for name, mutate := range map[string]func(*policy.Input){
		"no order flow":  func(in *policy.Input) { in.OrderFlow = domain.OrderFlowFeatures{} },
		"no 1m return":   func(in *policy.Input) { in.Return1m = nil },
		"no TDnet score": func(in *policy.Input) { in.TDnetScore = nil },
		"no Jev score":   func(in *policy.Input) { in.Decision.Confidence = nil },
	} {
		in := env.in
		d := *env.in.Decision
		in.Decision = &d
		mutate(&in)
		sig, err := newEngine(policytest.Option(policytest.ModelOf(policytest.TakingModel()), preds)).Evaluate(context.Background(), in)
		if err != nil {
			t.Fatalf("%s: Evaluate: %v", name, err)
		}
		if sig.Direction != domain.JevDirectionNone {
			t.Errorf("%s: Direction = %q, want NONE", name, sig.Direction)
		}
	}
}

func TestEvaluate_FeaturesAreOrientedTowardsTheCandidateSide(t *testing.T) {
	// A model that likes order flow in the candidate's favour: p = sigmoid(3*ofi_short).
	flowModel := policytest.ModelOf(policytest.LRModel(0, 3))
	for _, tc := range []struct {
		direction string
		ofi       float64
		want      string
	}{
		{domain.JevDirectionLong, 1, domain.JevDirectionLong},
		{domain.JevDirectionLong, -1, domain.JevDirectionNone},
		{domain.JevDirectionShort, -1, domain.JevDirectionShort}, // selling pressure favours a SHORT
		{domain.JevDirectionShort, 1, domain.JevDirectionNone},
	} {
		env := newMetaEnv(t, tc.direction)
		newEngine := env.newEngine
		in := env.in
		in.OrderFlow.OFIShort = ptr(tc.ofi)
		sig, err := newEngine(policytest.Option(flowModel, &policytest.Predictions{})).Evaluate(context.Background(), in)
		if err != nil || sig.Direction != tc.want {
			t.Errorf("%s with ofi_short %v: %+v, %v, want %s", tc.direction, tc.ofi, sig, err, tc.want)
		}
	}
}

func TestEvaluate_HardGuardsStillApplyBeforeTheModel(t *testing.T) {
	env := newMetaEnv(t, domain.JevDirectionLong)
	newEngine := env.newEngine
	preds := &policytest.Predictions{}
	in := env.in
	d := *env.in.Decision
	d.ToxicFlow = ptr(0.36) // above the 0.35 limit
	in.Decision = &d
	sig, err := newEngine(policytest.Option(policytest.ModelOf(policytest.TakingModel()), preds)).Evaluate(context.Background(), in)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if sig.Direction != domain.JevDirectionNone || !strings.HasPrefix(reasonOf(sig), policy.ReasonToxicFlowAboveThreshold) {
		t.Fatalf("signal = %+v (%s), want NONE for toxic flow even with a taking model", sig, reasonOf(sig))
	}
	if got := preds.Recorded(); len(got) != 0 {
		t.Errorf("predictions = %+v, want none for a candidate a guard rejected", got)
	}
}

func TestEvaluate_RiskEngineStillGatesAModelTake(t *testing.T) {
	env := newMetaEnv(t, domain.JevDirectionLong)
	preds := &policytest.Predictions{}
	e := policy.NewEngine(testThresholds(), fakeRiskChecker{passed: false, reason: "max_open_positions exceeded"}, env.signals, policytest.Option(policytest.ModelOf(policytest.TakingModel()), preds))

	sig, err := e.Evaluate(context.Background(), env.in)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if sig.Direction != domain.JevDirectionNone || sig.RiskPassed || !strings.HasPrefix(reasonOf(sig), policy.ReasonRiskEngineRejected) {
		t.Fatalf("signal = %+v (%s), want NONE from the Risk Engine's gate", sig, reasonOf(sig))
	}
	if got := preds.Recorded(); len(got) != 1 || got[0].Action != repo.ActionTake {
		t.Errorf("predictions = %+v, want the model's own take verdict recorded", got)
	}
}

func TestEvaluate_LogsTheComparisonWithTheLegacyThreshold(t *testing.T) {
	env := newMetaEnv(t, domain.JevDirectionLong)
	newEngine := env.newEngine
	buf := captureLogs(t)
	env.in.Decision.Confidence = ptr(0.40) // the legacy rule (0.68) would not enter

	if _, err := newEngine(policytest.Option(policytest.ModelOf(policytest.TakingModel()), &policytest.Predictions{})).Evaluate(context.Background(), env.in); err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	var line map[string]any
	for _, l := range decodeLogLines(t, buf) {
		if l["msg"] == "policy: meta-label vs legacy entry decision" {
			line = l
		}
	}
	if line == nil {
		t.Fatalf("no comparison line; lines: %+v", decodeLogLines(t, buf))
	}
	if line["legacy_direction"] != domain.JevDirectionNone || !strings.HasPrefix(line["legacy_reason"].(string), policy.ReasonProbabilityBelowThreshold) ||
		line["meta_direction"] != domain.JevDirectionLong || line["meta_action"] != repo.ActionTake || line["agree"] != false ||
		line["model_version"] != "lr-test" || line["jev_confidence"] != 0.40 {
		t.Errorf("comparison line = %+v", line)
	}
	if p, _ := line["calibrated_probability"].(float64); p < 0.87 {
		t.Errorf("calibrated_probability = %v", line["calibrated_probability"])
	}

	// Agreement: both enter.
	buf.Reset()
	env.in.Decision.Confidence = ptr(0.80)
	if _, err := newEngine(policytest.Option(policytest.ModelOf(policytest.TakingModel()), &policytest.Predictions{})).Evaluate(context.Background(), env.in); err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	for _, l := range decodeLogLines(t, buf) {
		if l["msg"] == "policy: meta-label vs legacy entry decision" && (l["agree"] != true || l["legacy_direction"] != domain.JevDirectionLong) {
			t.Errorf("comparison line = %+v, want agreement on LONG", l)
		}
	}
}

// Decide is the legacy threshold rule, kept only for the backtest's explicitly
// named legacy comparison mode (the replay itself uses DecideReplay): it is not
// changed by the meta-labeling entry rule.
func TestDecide_KeepsTheLegacyThresholdRule(t *testing.T) {
	e := policy.NewEngine(testThresholds(), nil, nil, policytest.Option(policytest.ModelOf(policytest.SkippingModel()), &policytest.Predictions{}))
	in := passingInput(domain.JevDirectionLong)
	if sig := e.Decide(context.Background(), in); sig.Direction != domain.JevDirectionLong || sig.PolicyVersion != policy.Version {
		t.Errorf("Decide = %+v, want the legacy LONG at the 0.68 boundary, version %q", sig, policy.Version)
	}
	in.Decision.Confidence = ptr(0.67)
	if sig := e.Decide(context.Background(), in); sig.Direction != domain.JevDirectionNone || !strings.HasPrefix(reasonOf(sig), policy.ReasonProbabilityBelowThreshold) {
		t.Errorf("Decide below 0.68 = %+v", sig)
	}
}

func TestInputFromSnapshot_CarriesTheModelInputs(t *testing.T) {
	snap := domain.Snapshot{Price: 100, Feature: policytest.Feature()}
	in := policy.InputFromSnapshot(snap)
	if in.OrderFlow.OFIShort == nil || *in.OrderFlow.OFIShort != 1 || in.OrderFlow.MicropriceDevBps == nil || *in.OrderFlow.MicropriceDevBps != 0.5 ||
		in.Return1m == nil || *in.Return1m != 0.001 {
		t.Errorf("Input = %+v, want the snapshot's order flow and 1-minute return", in)
	}
}
