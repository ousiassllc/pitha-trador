package metaflow_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/repository/trading"
	"github.com/ousiassllc/pitha-trador/internal/service/metalabel"
	"github.com/ousiassllc/pitha-trador/internal/service/policy"
	"github.com/ousiassllc/pitha-trador/internal/service/policy/policytest"
)

// metaEnv is a migrated database with one instrument and one persisted trader
// decision for it, an input for that decision and an Engine factory over the
// database's signal repository.
type metaEnv struct {
	db        *sql.DB
	signals   *trading.SignalRepository
	in        policy.Input
	newEngine func(...policy.Option) *policy.Engine
}

func newMetaEnv(t *testing.T, direction string) metaEnv {
	t.Helper()
	db := newDB(t)
	instrument, err := market.NewInstrumentRepository(db).Create(context.Background(), domain.Instrument{
		Symbol: "7203", Name: "Toyota", Market: "TSE Prime", IsActive: true,
	})
	if err != nil {
		t.Fatalf("Create instrument: %v", err)
	}
	in := passingInput(direction)
	in.InstrumentID = instrument.ID
	saved, err := judgement.NewDecisionRepository(db).Insert(context.Background(), domain.JevDecision{
		InstrumentID: instrument.ID, Symbol: "7203", Timestamp: in.Timestamp, DecisionType: domain.JevDecisionTypeTrader,
		StateHash: "hash", StateJSON: "{}", QuestionVersion: "v1", ResponseJSON: "{}", ModelID: "test-model",
	})
	if err != nil {
		t.Fatalf("Insert decision: %v", err)
	}
	in.Decision.ID = saved.ID
	signals := trading.NewSignalRepository(db)
	return metaEnv{db: db, signals: signals, in: in, newEngine: func(opts ...policy.Option) *policy.Engine {
		return policy.NewEngine(testThresholds(), nil, signals, opts...)
	}}
}

func reasonOf(sig domain.TradeSignal) string {
	if sig.RejectReason == nil {
		return ""
	}
	return *sig.RejectReason
}

func TestEvaluate_TakesOnlyWhenTheModelsCalibratedEVIsPositive(t *testing.T) {
	for name, tc := range map[string]struct {
		model      metalabel.Model
		wantTake   bool
		wantReason string
	}{
		"positive EV takes":     {policytest.TakingModel(), true, ""},
		"negative EV is a skip": {policytest.SkippingModel(), false, policy.ReasonMetaSkip + ": " + metalabel.ReasonEVBelowMin},
		// p = 0.4 is exactly EV 0 (0.4*30 - 0.6*20): not above zero.
		"zero EV is a skip": {policytest.LRModel(-0.4054651081081644), false, policy.ReasonMetaSkip + ": " + metalabel.ReasonEVBelowMin},
	} {
		t.Run(name, func(t *testing.T) {
			env := newMetaEnv(t, domain.JevDirectionLong)
			newEngine := env.newEngine
			preds := &policytest.Predictions{}
			sig, err := newEngine(policytest.Option(policytest.ModelOf(tc.model), preds)).Evaluate(context.Background(), env.in)
			if err != nil {
				t.Fatalf("Evaluate: %v", err)
			}
			if tc.wantTake {
				if sig.Direction != domain.JevDirectionLong || !sig.RiskPassed || sig.RejectReason != nil {
					t.Fatalf("signal = %+v, want a LONG entry", sig)
				}
				if sig.Score == nil || *sig.Score < 0.87 || *sig.Score > 0.89 {
					t.Errorf("Score = %v, want the calibrated probability (about 0.88)", sig.Score)
				}
			} else if sig.Direction != domain.JevDirectionNone || sig.RiskPassed || !strings.HasPrefix(reasonOf(sig), tc.wantReason) {
				t.Fatalf("signal = %+v (%s), want NONE with %q", sig, reasonOf(sig), tc.wantReason)
			}
			if sig.PolicyVersion != policy.MetaVersion {
				t.Errorf("PolicyVersion = %q, want %q", sig.PolicyVersion, policy.MetaVersion)
			}
		})
	}
}

func TestEvaluate_MinEVBpsFromTheSettingsRaisesTheBar(t *testing.T) {
	env := newMetaEnv(t, domain.JevDirectionLong)
	newEngine := env.newEngine
	cfg := metalabel.DefaultDecideConfig()
	e := newEngine(policytest.WithConfig(policytest.ModelOf(policytest.TakingModel()), &policytest.Predictions{}, func(context.Context) (metalabel.DecideConfig, error) { return cfg, nil }))

	if sig, err := e.Evaluate(context.Background(), env.in); err != nil || sig.Direction != domain.JevDirectionLong {
		t.Fatalf("min EV 0: %+v, %v, want LONG (EV is about 24 bps)", sig, err)
	}
	cfg.MinEVBps = 30 // read at the next decision, no restart
	sig, err := e.Evaluate(context.Background(), env.in)
	if err != nil || sig.Direction != domain.JevDirectionNone || !strings.Contains(reasonOf(sig), metalabel.ReasonEVBelowMin) {
		t.Fatalf("min EV 30: %+v, %v, want NONE for an EV below the configured minimum", sig, err)
	}
}

func TestEvaluate_JevConfidenceIsNoLongerAProbabilityThreshold(t *testing.T) {
	env := newMetaEnv(t, domain.JevDirectionLong)
	newEngine := env.newEngine
	e := newEngine(policytest.Option(policytest.ModelOf(policytest.TakingModel()), &policytest.Predictions{}))

	// Below the old 0.68 bar: the model's verdict decides.
	env.in.Decision.Confidence = ptr(0.40)
	if sig, err := e.Evaluate(context.Background(), env.in); err != nil || sig.Direction != domain.JevDirectionLong {
		t.Fatalf("confidence 0.40 with a taking model: %+v, %v, want LONG", sig, err)
	}
	// Far above it: a skipping model still skips.
	env.in.Decision.Confidence = ptr(0.99)
	skipper := newEngine(policytest.Option(policytest.ModelOf(policytest.SkippingModel()), &policytest.Predictions{}))
	if sig, err := skipper.Evaluate(context.Background(), env.in); err != nil || sig.Direction != domain.JevDirectionNone {
		t.Fatalf("confidence 0.99 with a skipping model: %+v, %v, want NONE", sig, err)
	}
}

func TestEvaluate_WithoutAModelIsSafeSideNone(t *testing.T) {
	env := newMetaEnv(t, domain.JevDirectionLong)
	newEngine := env.newEngine
	env.in.Decision.Confidence = ptr(0.99)
	preds := &policytest.Predictions{}
	for name, opts := range map[string][]policy.Option{
		"no active model": {policytest.Option(policytest.Models{}, preds)},
		"unusable model":  {policytest.Option(policytest.Models{Err: errors.New("feature layout changed")}, preds)},
		"model not wired": nil,
		"settings failure": {policy.WithMetaModel(policytest.ModelOf(policytest.TakingModel()), preds, func(context.Context) (metalabel.DecideConfig, error) {
			return metalabel.DecideConfig{}, errors.New("db down")
		})},
	} {
		sig, err := newEngine(opts...).Evaluate(context.Background(), env.in)
		if err != nil {
			t.Fatalf("%s: Evaluate: %v, want a recorded NONE", name, err)
		}
		if sig.Direction != domain.JevDirectionNone || sig.RiskPassed || sig.RejectReason == nil {
			t.Errorf("%s: signal = %+v, want NONE (never an entry without a model)", name, sig)
		}
		if name == "no active model" && !strings.HasPrefix(reasonOf(sig), policy.ReasonMetaSkip+": "+metalabel.ReasonNoModel) {
			t.Errorf("%s: reason = %q", name, reasonOf(sig))
		}
		if name != "no active model" && !strings.HasPrefix(reasonOf(sig), policy.ReasonMetaModelUnavailable) {
			t.Errorf("%s: reason = %q, want %s", name, reasonOf(sig), policy.ReasonMetaModelUnavailable)
		}
	}
	if got := preds.Recorded(); len(got) != 0 {
		t.Errorf("predictions = %+v, want none without a scoring model", got)
	}
}
