// Package apierrortests tests, through a real Handler, Trader and Engine, how
// the Policy Engine records a failed Jev call and a NONE answer (issue #912).
package apierrortests

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/jobqueue"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/repository/trading"
	"github.com/ousiassllc/pitha-trador/internal/service/jev"
	"github.com/ousiassllc/pitha-trador/internal/service/policy"
	"github.com/ousiassllc/pitha-trador/internal/service/policy/policytest"
	"github.com/ousiassllc/pitha-trador/internal/service/rag"
)

// fixtureNow is 10s after the fixture bar's 09:31, so the bar is fresh.
func fixtureNow() time.Time { return time.Date(2026, 9, 27, 9, 31, 10, 0, time.UTC) }

type fixture struct {
	db      *sql.DB
	handler *policy.Handler
	signals *trading.SignalRepository
	inst    domain.Instrument
	payload string // the jev-trader job payload of inst
}

// newFixture wires a Handler whose Trader calls the Jev API at baseURL (one
// attempt, perClientTimeout per request), with one instrument and a fresh bar.
func newFixture(t *testing.T, baseURL string, perAttempt time.Duration) fixture {
	t.Helper()
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha_test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	decisions := judgement.NewDecisionRepository(db)
	snapshots := market.NewSnapshotRepository(db)
	signals := trading.NewSignalRepository(db)
	client := jev.NewClient(jev.Config{BaseURL: baseURL, MaxAttempts: 1, HTTPClient: &http.Client{Timeout: perAttempt}})
	trader := jev.NewTrader(client, decisions, rag.NewService(db, decisions, snapshots))
	store := policytest.NewStore(t, db, policytest.TakingModel())
	engine := policy.NewEngine(thresholds(), nil, signals, policytest.Option(store, store))
	handler := policy.NewHandler(trader, snapshots, engine, nil, policy.WithClock(fixtureNow), policy.WithTDnetScorer(policytest.TDnet(0)))

	inst, err := market.NewInstrumentRepository(db).Create(context.Background(), domain.Instrument{Symbol: "7203", Name: "n", Market: "TSE Prime", IsActive: true})
	if err != nil {
		t.Fatal(err)
	}
	spread := 10.0
	if _, err := snapshots.Insert(context.Background(), domain.Snapshot{
		InstrumentID: inst.ID, Symbol: "7203", Timestamp: time.Date(2026, 9, 27, 9, 31, 0, 0, time.UTC),
		Price: 2110.5, SpreadBps: &spread, Volume: 1000, Turnover: 2_110_500, Feature: policytest.Feature(),
	}); err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(jev.ScoutJobPayload{InstrumentID: inst.ID, Symbol: "7203"})
	return fixture{db: db, handler: handler, signals: signals, inst: inst, payload: string(payload)}
}

func (f fixture) run(t *testing.T) error {
	t.Helper()
	return f.handler.HandleJob(context.Background(), jobqueue.Job{PayloadJSON: f.payload})
}

// onlySignal is the one signal row the job wrote.
func (f fixture) onlySignal(t *testing.T) domain.TradeSignal {
	t.Helper()
	got, err := f.signals.ListByInstrument(context.Background(), f.inst.ID, 10)
	if err != nil || len(got) != 1 {
		t.Fatalf("signals = %+v, %v; want exactly one", got, err)
	}
	return got[0]
}

func passingTraderResponse(direction string) jev.TraderResponse {
	return jev.TraderResponse{
		Direction: direction, Regime: domain.JevRegimeTrend, EntryQuality: domain.JevEntryQualityStrong,
		ToxicFlow: 0.35, LiquidityStressed: 0.25, ContinuationProbability: 0.60, Confidence: 0.68, ModelID: "test-model",
	}
}

func thresholds() policy.Thresholds {
	d := config.PolicyDirectionThresholds{
		MinProbability: 0.68, MinEntryQuality: "strong", MinContinuationProbability: 0.60, MaxToxicFlow: 0.35, MaxLiquidityStressed: 0.25,
	}
	return policy.Thresholds{Policy: config.PolicyConfig{Long: d, Short: d}, MaxSpreadBps: 50, MinTurnover5mJPY: 3_000_000}
}
