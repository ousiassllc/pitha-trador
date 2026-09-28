package policy_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository"
	"github.com/ousiassllc/pitha-trador/internal/service/jev"
	"github.com/ousiassllc/pitha-trador/internal/service/policy"
	"github.com/ousiassllc/pitha-trador/internal/service/rag"
)

func newHandlerTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := repository.Open(filepath.Join(t.TempDir(), "pitha_test.db"))
	if err != nil {
		t.Fatalf("repository.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// traderServer returns an httptest.Server that responds with resp to
// every Jev Trader call.
func traderServer(t *testing.T, resp jev.TraderResponse) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(server.Close)
	return server
}

// handlerFixture wires a real Trader (against an httptest server) and a
// real Engine (against a migrated SQLite DB) together, mirroring what
// jev-trader queue jobs run through in production once Scheduler
// registers Handler.HandleJob.
type handlerFixture struct {
	db       *sql.DB
	handler  *policy.Handler
	engine   *policy.Engine
	signals  *repository.SignalRepository
	jobs     *repository.JobRepository
	executor *recordingExecutor
}

// recordingExecutor records every signal Handler hands to its
// SignalExecutor.
type recordingExecutor struct {
	executed []domain.TradeSignal
}

func (e *recordingExecutor) ExecuteSignal(_ context.Context, signal domain.TradeSignal, _ domain.Snapshot) error {
	e.executed = append(e.executed, signal)
	return nil
}

func newHandlerFixture(t *testing.T, resp jev.TraderResponse) handlerFixture {
	t.Helper()
	db := newHandlerTestDB(t)

	decisions := repository.NewDecisionRepository(db)
	snapshots := repository.NewSnapshotRepository(db)
	signals := repository.NewSignalRepository(db)
	ragService := rag.NewService(db, decisions, snapshots)

	server := traderServer(t, resp)
	client := jev.NewClient(jev.Config{BaseURL: server.URL, MaxAttempts: 1})
	trader := jev.NewTrader(client, decisions, ragService)

	engine := policy.NewEngine(testThresholds(), nil, signals)
	executor := &recordingExecutor{}
	handler := policy.NewHandler(trader, snapshots, engine, executor)

	return handlerFixture{db: db, handler: handler, engine: engine, signals: signals, jobs: repository.NewJobRepository(db), executor: executor}
}

func mustCreateInstrumentAndSnapshot(t *testing.T, db *sql.DB, symbol string, spreadBps float64) domain.Instrument {
	t.Helper()
	inst, err := repository.NewInstrumentRepository(db).Create(context.Background(), domain.Instrument{
		Symbol: symbol, Name: symbol + " Inc", Market: "TSE Prime", IsActive: true,
	})
	if err != nil {
		t.Fatalf("create instrument fixture: %v", err)
	}

	_, err = repository.NewSnapshotRepository(db).Insert(context.Background(), domain.Snapshot{
		InstrumentID: inst.ID, Symbol: symbol,
		Timestamp: time.Date(2026, 9, 27, 9, 31, 0, 0, time.UTC),
		Price:     2110.5, SpreadBps: ptr(spreadBps), Volume: 1000, Turnover: 2_110_500,
	})
	if err != nil {
		t.Fatalf("create snapshot fixture: %v", err)
	}
	return inst
}

func passingTraderResponse(direction string) jev.TraderResponse {
	return jev.TraderResponse{
		Direction: direction, Regime: domain.JevRegimeTrend, EntryQuality: domain.JevEntryQualityStrong,
		ToxicFlow: 0.35, LiquidityStressed: 0.25, ContinuationProbability: 0.60, Confidence: 0.68,
		ModelID: "test-model",
	}
}

func TestHandler_HandleJob_PersistsLongTradeSignalFromRealTraderCall(t *testing.T) {
	f := newHandlerFixture(t, passingTraderResponse(domain.JevDirectionLong))
	inst := mustCreateInstrumentAndSnapshot(t, f.db, "7203", 10)

	payloadJSON, err := json.Marshal(jev.ScoutJobPayload{InstrumentID: inst.ID, Symbol: "7203"})
	if err != nil {
		t.Fatalf("marshal job payload: %v", err)
	}

	if err := f.handler.HandleJob(context.Background(), repository.Job{PayloadJSON: string(payloadJSON)}); err != nil {
		t.Fatalf("HandleJob: %v", err)
	}

	signals, err := f.signals.ListByInstrument(context.Background(), inst.ID, 10)
	if err != nil {
		t.Fatalf("ListByInstrument: %v", err)
	}
	if len(signals) != 1 {
		t.Fatalf("ListByInstrument() = %d rows, want 1", len(signals))
	}
	if signals[0].Direction != domain.JevDirectionLong {
		t.Fatalf("Direction = %q, want %q", signals[0].Direction, domain.JevDirectionLong)
	}
	if signals[0].JevDecisionID == nil {
		t.Fatalf("JevDecisionID = nil, want a reference to the persisted jev_decisions row")
	}
	if !signals[0].RiskPassed {
		t.Fatalf("RiskPassed = false, want true")
	}
	if len(f.executor.executed) != 1 || f.executor.executed[0].ID != signals[0].ID {
		t.Fatalf("executed signals = %+v, want exactly the persisted LONG signal (id %d)", f.executor.executed, signals[0].ID)
	}
}

func TestHandler_HandleJob_PersistsNoneTradeSignalWhenSpreadTooWide(t *testing.T) {
	f := newHandlerFixture(t, passingTraderResponse(domain.JevDirectionLong))
	inst := mustCreateInstrumentAndSnapshot(t, f.db, "7203", 999)

	payloadJSON, _ := json.Marshal(jev.ScoutJobPayload{InstrumentID: inst.ID, Symbol: "7203"})

	if err := f.handler.HandleJob(context.Background(), repository.Job{PayloadJSON: string(payloadJSON)}); err != nil {
		t.Fatalf("HandleJob: %v", err)
	}

	signals, err := f.signals.ListByInstrument(context.Background(), inst.ID, 10)
	if err != nil {
		t.Fatalf("ListByInstrument: %v", err)
	}
	if len(signals) != 1 || signals[0].Direction != domain.JevDirectionNone {
		t.Fatalf("signals = %+v, want a single NONE row", signals)
	}
	if len(f.executor.executed) != 0 {
		t.Fatalf("executed signals = %+v, want none for a NONE signal", f.executor.executed)
	}
}

func TestHandler_HandleJob_PersistsNoneTradeSignalAndReturnsErrorOnJevAPIFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	db := newHandlerTestDB(t)
	decisions := repository.NewDecisionRepository(db)
	snapshots := repository.NewSnapshotRepository(db)
	signals := repository.NewSignalRepository(db)
	ragService := rag.NewService(db, decisions, snapshots)

	client := jev.NewClient(jev.Config{BaseURL: server.URL, MaxAttempts: 1})
	trader := jev.NewTrader(client, decisions, ragService)
	engine := policy.NewEngine(testThresholds(), nil, signals)
	handler := policy.NewHandler(trader, snapshots, engine, nil)

	inst := mustCreateInstrumentAndSnapshot(t, db, "7203", 10)
	payloadJSON, _ := json.Marshal(jev.ScoutJobPayload{InstrumentID: inst.ID, Symbol: "7203"})

	err := handler.HandleJob(context.Background(), repository.Job{PayloadJSON: string(payloadJSON)})
	if err == nil {
		t.Fatal("HandleJob: want error when the Jev API call fails, got nil")
	}

	got, err := signals.ListByInstrument(context.Background(), inst.ID, 10)
	if err != nil {
		t.Fatalf("ListByInstrument: %v", err)
	}
	if len(got) != 1 || got[0].Direction != domain.JevDirectionNone {
		t.Fatalf("signals = %+v, want a single NONE row recording the api_error", got)
	}
	if got[0].RejectReason == nil {
		t.Fatalf("RejectReason = nil, want an api_error explanation")
	}
}
