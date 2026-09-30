package jev_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/service/jev"
	"github.com/ousiassllc/pitha-trador/internal/service/rag"
)

type fakeNews map[string]domain.NewsContext

func (f fakeNews) NewsContext(symbol string) (domain.NewsContext, bool) {
	ctx, ok := f[symbol]
	return ctx, ok
}

func newsFor7203() fakeNews {
	return fakeNews{"7203": domain.NewsContext{Items: []domain.NewsContextItem{{
		Sentiment: domain.NewsSentimentBullish, EventType: domain.NewsEventEarnings, Summary: "上方修正",
		PublishedAt: time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC),
	}}}}
}

func stateJSONNewsContext(t *testing.T, stateJSON string) *domain.NewsContext {
	t.Helper()
	var decoded struct {
		NewsContext *domain.NewsContext `json:"news_context"`
	}
	if err := json.Unmarshal([]byte(stateJSON), &decoded); err != nil {
		t.Fatalf("unmarshal state_json: %v", err)
	}
	return decoded.NewsContext
}

func TestScout_Evaluate_InjectsNewsContextIntoStateJSONAndRequest(t *testing.T) {
	var sent jev.ScoutRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&sent)
		_ = json.NewEncoder(w).Encode(jev.ScoutResponse{InterestingNow: 0.8, LiquidityOk: 0.9, AbnormalActivity: 0.6})
	}))
	t.Cleanup(server.Close)

	db := newTestDB(t)
	decisions := judgement.NewDecisionRepository(db)
	inst := mustCreateInstrument(t, market.NewInstrumentRepository(db), "7203")
	ragService := rag.NewService(db, decisions, market.NewSnapshotRepository(db))
	scout := jev.NewScout(jev.NewClient(jev.Config{BaseURL: server.URL}), decisions, market.NewSnapshotRepository(db), nil, ragService, testThresholds(), jev.WithNewsSource(newsFor7203()))

	decision, passed, err := scout.Evaluate(context.Background(), inst.ID, jev.ScoutState{Symbol: "7203"})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	// FR-LUNA-5: news only adds context; Jev's own answer decides pass/fail.
	if !passed {
		t.Error("passed = false, want Jev's own verdict (true) unaffected by news")
	}
	got := stateJSONNewsContext(t, decision.StateJSON)
	if got == nil || len(got.Items) != 1 || got.Items[0].Summary != "上方修正" {
		t.Errorf("state_json news_context = %+v, want the injected Luna item", got)
	}
	if sent.State.NewsContext == nil {
		t.Error("Jev request state has no news_context, want it injected")
	}
}

func TestScout_Evaluate_NoNewsForSymbolLeavesStateWithoutNewsContext(t *testing.T) {
	server := scoutServer(t, jev.ScoutResponse{InterestingNow: 0.8, LiquidityOk: 0.9, AbnormalActivity: 0.6})
	db := newTestDB(t)
	decisions := judgement.NewDecisionRepository(db)
	inst := mustCreateInstrument(t, market.NewInstrumentRepository(db), "9433")
	ragService := rag.NewService(db, decisions, market.NewSnapshotRepository(db))
	scout := jev.NewScout(jev.NewClient(jev.Config{BaseURL: server.URL}), decisions, market.NewSnapshotRepository(db), nil, ragService, testThresholds(), jev.WithNewsSource(newsFor7203()))

	decision, _, err := scout.Evaluate(context.Background(), inst.ID, jev.ScoutState{Symbol: "9433"})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if got := stateJSONNewsContext(t, decision.StateJSON); got != nil {
		t.Errorf("state_json news_context = %+v, want none when the symbol has no news", got)
	}
}

func TestTrader_Evaluate_InjectsNewsContextWithoutChangingJevJudgment(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(jev.TraderResponse{
			Direction: domain.JevDirectionShort, Regime: domain.JevRegimeTrend, EntryQuality: domain.JevEntryQualityGood, Confidence: 0.7,
		})
	}))
	t.Cleanup(server.Close)

	db := newTestDB(t)
	decisions := judgement.NewDecisionRepository(db)
	inst := mustCreateInstrument(t, market.NewInstrumentRepository(db), "7203")
	ragService := rag.NewService(db, decisions, market.NewSnapshotRepository(db))
	trader := jev.NewTrader(jev.NewClient(jev.Config{BaseURL: server.URL}), decisions, ragService, jev.WithNewsSource(newsFor7203()))

	decision, err := trader.Evaluate(context.Background(), inst.ID, jev.ScoutState{Symbol: "7203"})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if got := stateJSONNewsContext(t, decision.StateJSON); got == nil {
		t.Error("state_json has no news_context, want it injected")
	}
	// A bullish news item must not turn Jev's SHORT into anything else (FR-LUNA-5).
	if decision.Direction == nil || *decision.Direction != domain.JevDirectionShort {
		t.Errorf("Direction = %v, want Jev's own SHORT", decision.Direction)
	}
}
