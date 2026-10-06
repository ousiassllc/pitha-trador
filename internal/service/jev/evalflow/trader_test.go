package evalflow_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/service/jev"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/jevtest"
	"github.com/ousiassllc/pitha-trador/internal/service/rag"
)

// traderServer returns an httptest.Server that responds with resp to
// every Jev Trader call.
func traderServer(t *testing.T, resp jev.TraderResponse) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(jevtest.TraderHandler(resp))
	t.Cleanup(server.Close)
	return server
}

func TestTrader_Evaluate_PersistsDecisionWithAllFields(t *testing.T) {
	tests := map[string]jev.TraderResponse{
		"LONG/BREAKOUT/exceptional": {
			Direction: domain.JevDirectionLong, Regime: domain.JevRegimeBreakout,
			EntryQuality: domain.JevEntryQualityExceptional, Confidence: 0.82,
			ToxicFlow: 0.12, LiquidityStressed: 0.08, ContinuationProbability: 0.71,
			ModelID: "jev-trader-test",
		},
		"SHORT/TREND/strong": {
			Direction: domain.JevDirectionShort, Regime: domain.JevRegimeTrend,
			EntryQuality: domain.JevEntryQualityStrong, Confidence: 0.70,
			ToxicFlow: 0.30, LiquidityStressed: 0.20, ContinuationProbability: 0.61,
			ModelID: "jev-trader-test",
		},
		"NONE/CHAOTIC/poor": {
			Direction: domain.JevDirectionNone, Regime: domain.JevRegimeChaotic,
			EntryQuality: domain.JevEntryQualityPoor, Confidence: 0.30,
			ToxicFlow: 0.60, LiquidityStressed: 0.55, ContinuationProbability: 0.20,
			ModelID: "jev-trader-test",
		},
		"NONE/RANGE/fair": {
			Direction: domain.JevDirectionNone, Regime: domain.JevRegimeRange,
			EntryQuality: domain.JevEntryQualityFair, Confidence: 0.45,
			ToxicFlow: 0.40, LiquidityStressed: 0.35, ContinuationProbability: 0.35,
			ModelID: "jev-trader-test",
		},
	}

	for name, resp := range tests {
		t.Run(name, func(t *testing.T) {
			server := traderServer(t, resp)

			db := newTestDB(t)
			instruments := market.NewInstrumentRepository(db)
			decisions := judgement.NewDecisionRepository(db)
			inst := mustCreateInstrument(t, instruments, "7203")

			client := jev.NewClient(jev.Config{BaseURL: server.URL})
			ragService := rag.NewService(db, decisions, market.NewSnapshotRepository(db))
			trader := jev.NewTrader(client, decisions, ragService)

			state := jev.ScoutState{Symbol: "7203", Timestamp: time.Date(2026, 9, 27, 9, 31, 0, 0, time.UTC), Price: 2100}
			decision, err := trader.Evaluate(context.Background(), inst.ID, state)
			if err != nil {
				t.Fatalf("Evaluate: %v", err)
			}

			if decision.ID == 0 {
				t.Fatalf("Evaluate() decision.ID = 0, want a persisted row id")
			}
			if decision.DecisionType != domain.JevDecisionTypeTrader {
				t.Errorf("decision.DecisionType = %q, want %q", decision.DecisionType, domain.JevDecisionTypeTrader)
			}
			if decision.QuestionVersion != jev.TraderQuestionVersion {
				t.Errorf("decision.QuestionVersion = %q, want %q", decision.QuestionVersion, jev.TraderQuestionVersion)
			}
			if decision.ModelID != resp.ModelID {
				t.Errorf("decision.ModelID = %q, want %q", decision.ModelID, resp.ModelID)
			}
			if decision.StateHash == "" {
				t.Error("decision.StateHash is empty, want a computed hash")
			}

			assertDeref(t, "Direction", decision.Direction, resp.Direction)
			assertDeref(t, "Regime", decision.Regime, resp.Regime)
			assertDeref(t, "EntryQuality", decision.EntryQuality, resp.EntryQuality)
			assertDerefFloat(t, "Confidence", decision.Confidence, resp.Confidence)
			assertDerefFloat(t, "ToxicFlow", decision.ToxicFlow, resp.ToxicFlow)
			assertDerefFloat(t, "LiquidityStressed", decision.LiquidityStressed, resp.LiquidityStressed)
			assertDerefFloat(t, "ContinuationProbability", decision.ContinuationProbability, resp.ContinuationProbability)

			saved, err := decisions.Get(context.Background(), decision.ID)
			if err != nil {
				t.Fatalf("Get(%d): %v", decision.ID, err)
			}
			if saved.ResponseJSON == "" || saved.StateJSON == "" {
				t.Errorf("Get(%d) = %+v, want non-empty StateJSON/ResponseJSON", decision.ID, saved)
			}
			// FR-TRADER-3: direction/confidence are the two jev_decisions
			// columns Trader shares with Scout's schema - verify they
			// round-trip through the DB, not just the in-memory return
			// value.
			assertDeref(t, "saved.Direction", saved.Direction, resp.Direction)
			assertDerefFloat(t, "saved.Confidence", saved.Confidence, resp.Confidence)
		})
	}
}

func TestTrader_Evaluate_APIFailurePersistsNothing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	db := newTestDB(t)
	instruments := market.NewInstrumentRepository(db)
	decisions := judgement.NewDecisionRepository(db)
	inst := mustCreateInstrument(t, instruments, "1301")

	client := jev.NewClient(jev.Config{BaseURL: server.URL, MaxAttempts: 1})
	ragService := rag.NewService(db, decisions, market.NewSnapshotRepository(db))
	trader := jev.NewTrader(client, decisions, ragService)

	_, err := trader.Evaluate(context.Background(), inst.ID, jev.ScoutState{Symbol: "1301"})
	if err == nil {
		t.Fatal("Evaluate: want error when the Jev API call fails, got nil")
	}

	got, err := decisions.ListByInstrument(context.Background(), inst.ID, 10)
	if err != nil {
		t.Fatalf("ListByInstrument: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("ListByInstrument() = %d rows, want 0 (継続失敗でnew entry停止: a failed call persists nothing)", len(got))
	}
}

func assertDeref(t *testing.T, field string, got *string, want string) {
	t.Helper()
	if got == nil {
		t.Fatalf("decision.%s = nil, want %q", field, want)
	}
	if *got != want {
		t.Errorf("decision.%s = %q, want %q", field, *got, want)
	}
}

func assertDerefFloat(t *testing.T, field string, got *float64, want float64) {
	t.Helper()
	if got == nil {
		t.Fatalf("decision.%s = nil, want %v", field, want)
	}
	if *got != want {
		t.Errorf("decision.%s = %v, want %v", field, *got, want)
	}
}
