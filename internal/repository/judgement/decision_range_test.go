package judgement_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

func TestDecisionRepository_EachInRange_HalfOpenOldestFirstBothTypes(t *testing.T) {
	ctx := context.Background()
	repo, instID := openTestDecisionRepo(t)
	base := time.Date(2026, 9, 28, 0, 30, 0, 0, time.UTC)

	for i, at := range []time.Time{base.Add(2 * time.Minute), base, base.Add(time.Minute), base.Add(-time.Minute)} {
		typ := domain.JevDecisionTypeTrader
		if i%2 == 1 {
			typ = domain.JevDecisionTypeScout
		}
		if _, err := repo.Insert(ctx, domain.JevDecision{
			InstrumentID: instID, Symbol: "7203", Timestamp: at, DecisionType: typ, StateHash: "h", StateJSON: `{"a":1}`,
			QuestionVersion: "v1", ResponseJSON: `{"b":2}`, LatencyMs: 10 + i, ModelID: "m",
		}); err != nil {
			t.Fatalf("insert decision: %v", err)
		}
	}

	var got []domain.JevDecision
	if err := repo.EachInRange(ctx, base, base.Add(2*time.Minute), func(d domain.JevDecision) error {
		got = append(got, d)
		return nil
	}); err != nil {
		t.Fatalf("EachInRange: %v", err)
	}
	if len(got) != 2 || !got[0].Timestamp.Equal(base) || !got[1].Timestamp.Equal(base.Add(time.Minute)) {
		t.Fatalf("decisions in [base, base+2m) = %+v, want base then base+1m", got)
	}
	if got[0].DecisionType == got[1].DecisionType {
		t.Errorf("types = %s, %s, want both Scout and Trader included", got[0].DecisionType, got[1].DecisionType)
	}
	if got[0].StateJSON != `{"a":1}` || got[0].ResponseJSON != `{"b":2}` || got[0].LatencyMs == 0 {
		t.Errorf("request/response/latency not read: %+v", got[0])
	}

	stop := errors.New("stop")
	if err := repo.EachInRange(ctx, base, base.Add(time.Hour), func(domain.JevDecision) error { return stop }); !errors.Is(err, stop) {
		t.Fatalf("err = %v, want it to wrap fn's error", err)
	}
}
