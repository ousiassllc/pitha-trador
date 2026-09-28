package policy_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository"
	"github.com/ousiassllc/pitha-trador/internal/service/policy"
)

// mutablePolicySource returns whatever current holds at call time,
// standing in for runtime_settings changing between two evaluations.
type mutablePolicySource struct {
	current config.PolicyConfig
	err     error
}

func (s *mutablePolicySource) CurrentThresholds(context.Context) (config.PolicyConfig, error) {
	return s.current, s.err
}

func TestEngine_Evaluate_UsesPolicySourceThresholdsAtEachCall(t *testing.T) {
	db := newHandlerTestDB(t)
	instrument, err := repository.NewInstrumentRepository(db).Create(context.Background(), domain.Instrument{
		Symbol: "7203", Name: "Toyota", Market: "TSE Prime", IsActive: true,
	})
	if err != nil {
		t.Fatalf("Create instrument: %v", err)
	}
	in := passingInput(domain.JevDirectionLong) // confidence exactly 0.68
	in.InstrumentID = instrument.ID
	saved, err := repository.NewDecisionRepository(db).Insert(context.Background(), domain.JevDecision{
		InstrumentID: instrument.ID, Symbol: "7203", Timestamp: in.Timestamp, DecisionType: domain.JevDecisionTypeTrader,
		StateHash: "hash", StateJSON: "{}", QuestionVersion: "v1", ResponseJSON: "{}", ModelID: "test-model",
	})
	if err != nil {
		t.Fatalf("Insert decision: %v", err)
	}
	in.Decision.ID = saved.ID

	source := &mutablePolicySource{current: testThresholds().Policy}
	engine := policy.NewEngine(testThresholds(), nil, repository.NewSignalRepository(db), policy.WithPolicySource(source))

	first, err := engine.Evaluate(context.Background(), in)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if first.Direction != domain.JevDirectionLong {
		t.Fatalf("first Direction = %q, want LONG at the 0.68 boundary", first.Direction)
	}

	// An applied proposal raises long.min_probability past the decision.
	source.current.Long.MinProbability = 0.70
	second, err := engine.Evaluate(context.Background(), in)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if second.Direction != domain.JevDirectionNone {
		t.Errorf("second Direction = %q, want NONE once the source's min_probability is 0.70", second.Direction)
	}
}

func TestEngine_Evaluate_PolicySourceErrorFailsWithoutPersisting(t *testing.T) {
	db := newHandlerTestDB(t)
	signals := repository.NewSignalRepository(db)
	readErr := errors.New("runtime_settings unavailable")
	engine := policy.NewEngine(testThresholds(), nil, signals, policy.WithPolicySource(&mutablePolicySource{err: readErr}))

	if _, err := engine.Evaluate(context.Background(), passingInput(domain.JevDirectionLong)); !errors.Is(err, readErr) {
		t.Fatalf("Evaluate error = %v, want it to wrap the policy source error", err)
	}
	if got, err := signals.ListByInstrument(context.Background(), 1, 10); err != nil || len(got) != 0 {
		t.Errorf("persisted signals = %v (err %v), want none when thresholds cannot be read", got, err)
	}
}
