package jev_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/jobqueue"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/service/jev"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/jevtest"
	"github.com/ousiassllc/pitha-trador/internal/service/rag"
)

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha_test.db"))
	if err != nil {
		t.Fatalf("sqlitedb.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func testThresholds() config.JevScoutConfig {
	return config.JevScoutConfig{MinInterestingNow: 0.65, MinLiquidityOk: 0.70, MinAbnormalActivity: 0.55}
}

// scoutServer returns an httptest.Server that responds with resp to every
// Jev Scout call.
func scoutServer(t *testing.T, resp jev.ScoutResponse) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(jevtest.ScoutHandler(resp))
	t.Cleanup(server.Close)
	return server
}

func mustCreateInstrument(t *testing.T, instruments *market.InstrumentRepository, symbol string) domain.Instrument {
	t.Helper()
	inst, err := instruments.Create(context.Background(), domain.Instrument{
		Symbol: symbol, Name: symbol + " Inc", Market: "TSE Prime", IsActive: true,
	})
	if err != nil {
		t.Fatalf("create instrument %q: %v", symbol, err)
	}
	return inst
}

func TestPasses(t *testing.T) {
	cfg := testThresholds()
	tests := map[string]struct {
		resp jev.ScoutResponse
		want bool
	}{
		"all thresholds cleared":      {jev.ScoutResponse{InterestingNow: 0.65, LiquidityOk: 0.70, AbnormalActivity: 0.55}, true},
		"interesting_now below min":   {jev.ScoutResponse{InterestingNow: 0.64, LiquidityOk: 0.90, AbnormalActivity: 0.90}, false},
		"liquidity_ok below min":      {jev.ScoutResponse{InterestingNow: 0.90, LiquidityOk: 0.69, AbnormalActivity: 0.90}, false},
		"abnormal_activity below min": {jev.ScoutResponse{InterestingNow: 0.90, LiquidityOk: 0.90, AbnormalActivity: 0.54}, false},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if got := jev.Passes(cfg, tt.resp); got != tt.want {
				t.Errorf("Passes(%+v) = %v, want %v", tt.resp, got, tt.want)
			}
		})
	}
}

func TestScout_Evaluate_PersistsDecisionAndReportsPass(t *testing.T) {
	server := scoutServer(t, jev.ScoutResponse{
		InterestingNow: 0.8, MomentumQuality: jev.MomentumQualityStrong,
		LiquidityOk: 0.9, AbnormalActivity: 0.6, ModelID: "jev-scout-test",
	})

	db := newTestDB(t)
	instruments := market.NewInstrumentRepository(db)
	decisions := judgement.NewDecisionRepository(db)
	inst := mustCreateInstrument(t, instruments, "7203")

	client := jev.NewClient(jev.Config{BaseURL: server.URL})
	ragService := rag.NewService(db, decisions, market.NewSnapshotRepository(db))
	scout := jev.NewScout(client, decisions, market.NewSnapshotRepository(db), nil, ragService, testThresholds())

	state := jev.ScoutState{Symbol: "7203", Timestamp: time.Date(2026, 9, 27, 9, 31, 0, 0, time.UTC), Price: 2100}
	decision, passed, err := scout.Evaluate(context.Background(), inst.ID, state)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if !passed {
		t.Errorf("Evaluate() passed = false, want true for interesting_now=0.8/liquidity_ok=0.9/abnormal_activity=0.6")
	}
	if decision.ID == 0 {
		t.Fatalf("Evaluate() decision.ID = 0, want a persisted row id")
	}
	if decision.DecisionType != domain.JevDecisionTypeScout {
		t.Errorf("decision.DecisionType = %q, want %q", decision.DecisionType, domain.JevDecisionTypeScout)
	}
	if decision.ModelID != "jev-scout-test" {
		t.Errorf("decision.ModelID = %q, want %q", decision.ModelID, "jev-scout-test")
	}
	if decision.QuestionVersion != jev.ScoutQuestionVersion {
		t.Errorf("decision.QuestionVersion = %q, want %q", decision.QuestionVersion, jev.ScoutQuestionVersion)
	}
	if decision.StateHash == "" {
		t.Error("decision.StateHash is empty, want a computed hash")
	}

	saved, err := decisions.Get(context.Background(), decision.ID)
	if err != nil {
		t.Fatalf("Get(%d): %v", decision.ID, err)
	}
	if saved.ResponseJSON == "" || saved.StateJSON == "" {
		t.Errorf("Get(%d) = %+v, want non-empty StateJSON/ResponseJSON", decision.ID, saved)
	}
}

func TestScout_Evaluate_FailsToPassBelowThreshold(t *testing.T) {
	server := scoutServer(t, jev.ScoutResponse{InterestingNow: 0.5, LiquidityOk: 0.9, AbnormalActivity: 0.9})

	db := newTestDB(t)
	instruments := market.NewInstrumentRepository(db)
	decisions := judgement.NewDecisionRepository(db)
	inst := mustCreateInstrument(t, instruments, "9433")

	ragService := rag.NewService(db, decisions, market.NewSnapshotRepository(db))
	scout := jev.NewScout(jev.NewClient(jev.Config{BaseURL: server.URL}), decisions, market.NewSnapshotRepository(db), nil, ragService, testThresholds())

	_, passed, err := scout.Evaluate(context.Background(), inst.ID, jev.ScoutState{Symbol: "9433"})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if passed {
		t.Error("Evaluate() passed = true, want false for interesting_now=0.5 (below 0.65 threshold)")
	}
}

func TestScout_Evaluate_APIFailurePersistsNothing(t *testing.T) {
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
	scout := jev.NewScout(client, decisions, market.NewSnapshotRepository(db), nil, ragService, testThresholds())

	_, _, err := scout.Evaluate(context.Background(), inst.ID, jev.ScoutState{Symbol: "1301"})
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

func TestScout_HandleJob_EnqueuesJevTraderJobOnPass(t *testing.T) {
	server := scoutServer(t, jev.ScoutResponse{InterestingNow: 0.9, LiquidityOk: 0.9, AbnormalActivity: 0.9})

	db := newTestDB(t)
	instruments := market.NewInstrumentRepository(db)
	snapshots := market.NewSnapshotRepository(db)
	jobs := jobqueue.NewJobRepository(db)
	inst := mustCreateInstrument(t, instruments, "7203")

	if _, err := snapshots.Insert(context.Background(), domain.Snapshot{
		InstrumentID: inst.ID, Symbol: inst.Symbol,
		Timestamp: time.Date(2026, 9, 27, 9, 31, 0, 0, time.UTC),
		Price:     2100, Volume: 1000, Turnover: 2_000_000,
		Feature:     domain.Feature{VWAP: 2090, PriceVsVWAPBps: 47.8},
		RawDataJSON: `{}`,
	}); err != nil {
		t.Fatalf("insert snapshot fixture: %v", err)
	}

	ragService := rag.NewService(db, judgement.NewDecisionRepository(db), snapshots)
	scout := jev.NewScout(jev.NewClient(jev.Config{BaseURL: server.URL}), judgement.NewDecisionRepository(db), snapshots, jobs, ragService, testThresholds())

	payload, err := json.Marshal(jev.ScoutJobPayload{InstrumentID: inst.ID, Symbol: inst.Symbol})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	job, err := jobs.Enqueue(context.Background(), jobqueue.JobQueueJevScout, string(payload), time.Now().UTC())
	if err != nil {
		t.Fatalf("enqueue scout job: %v", err)
	}

	if err := scout.HandleJob(context.Background(), job); err != nil {
		t.Fatalf("HandleJob: %v", err)
	}

	traderJob, err := jobs.ClaimNext(context.Background(), jobqueue.JobQueueJevTrader, time.Now().UTC())
	if err != nil {
		t.Fatalf("ClaimNext(jev-trader): %v, want an enqueued jev-trader job after a Scout pass", err)
	}
	var traderPayload jev.ScoutJobPayload
	if err := json.Unmarshal([]byte(traderJob.PayloadJSON), &traderPayload); err != nil {
		t.Fatalf("unmarshal jev-trader job payload: %v", err)
	}
	if traderPayload.InstrumentID != inst.ID || traderPayload.Symbol != inst.Symbol {
		t.Errorf("jev-trader job payload = %+v, want instrument_id=%d symbol=%q", traderPayload, inst.ID, inst.Symbol)
	}
}

func TestScout_HandleJob_NoJevTraderJobOnFail(t *testing.T) {
	server := scoutServer(t, jev.ScoutResponse{InterestingNow: 0.1, LiquidityOk: 0.1, AbnormalActivity: 0.1})

	db := newTestDB(t)
	instruments := market.NewInstrumentRepository(db)
	snapshots := market.NewSnapshotRepository(db)
	jobs := jobqueue.NewJobRepository(db)
	inst := mustCreateInstrument(t, instruments, "9433")

	if _, err := snapshots.Insert(context.Background(), domain.Snapshot{
		InstrumentID: inst.ID, Symbol: inst.Symbol,
		Timestamp: time.Date(2026, 9, 27, 9, 31, 0, 0, time.UTC),
		Price:     1000, Volume: 500, Turnover: 500_000,
		Feature:     domain.Feature{VWAP: 995, PriceVsVWAPBps: 50.3},
		RawDataJSON: `{}`,
	}); err != nil {
		t.Fatalf("insert snapshot fixture: %v", err)
	}

	ragService := rag.NewService(db, judgement.NewDecisionRepository(db), snapshots)
	scout := jev.NewScout(jev.NewClient(jev.Config{BaseURL: server.URL}), judgement.NewDecisionRepository(db), snapshots, jobs, ragService, testThresholds())

	payload, _ := json.Marshal(jev.ScoutJobPayload{InstrumentID: inst.ID, Symbol: inst.Symbol})
	job, err := jobs.Enqueue(context.Background(), jobqueue.JobQueueJevScout, string(payload), time.Now().UTC())
	if err != nil {
		t.Fatalf("enqueue scout job: %v", err)
	}

	if err := scout.HandleJob(context.Background(), job); err != nil {
		t.Fatalf("HandleJob: %v", err)
	}

	if _, err := jobs.ClaimNext(context.Background(), jobqueue.JobQueueJevTrader, time.Now().UTC()); err == nil {
		t.Fatal("ClaimNext(jev-trader) succeeded, want no job enqueued after a Scout fail")
	}
}

func TestScout_HandleJob_NoSnapshotReturnsError(t *testing.T) {
	server := scoutServer(t, jev.ScoutResponse{})

	db := newTestDB(t)
	instruments := market.NewInstrumentRepository(db)
	inst := mustCreateInstrument(t, instruments, "1301")

	ragService := rag.NewService(db, judgement.NewDecisionRepository(db), market.NewSnapshotRepository(db))
	scout := jev.NewScout(
		jev.NewClient(jev.Config{BaseURL: server.URL}),
		judgement.NewDecisionRepository(db),
		market.NewSnapshotRepository(db),
		jobqueue.NewJobRepository(db),
		ragService,
		testThresholds(),
	)
	payload, _ := json.Marshal(jev.ScoutJobPayload{InstrumentID: inst.ID, Symbol: inst.Symbol})
	err := scout.HandleJob(context.Background(), jobqueue.Job{PayloadJSON: string(payload)})
	if err == nil {
		t.Fatal("HandleJob: want error when no market_snapshots row exists yet, got nil")
	}
}
