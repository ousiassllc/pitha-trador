package replayflow_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/jevreplaysettings"
	"github.com/ousiassllc/pitha-trador/internal/config/jevtrialsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/domain/jevreplay"
	repo "github.com/ousiassllc/pitha-trador/internal/repository/jevreplay"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/trialwire"
	service "github.com/ousiassllc/pitha-trador/internal/service/jevreplay"
)

// now is the fixed clock of the tests (2026-11-15 12:00 JST).
var now = time.Date(2026, 11, 15, 3, 0, 0, 0, time.UTC)

// fakeArm is one arm's model client: fn answers each call, every saved input it
// received is recorded. It only ever sees its own inputs.
type fakeArm struct {
	mu         sync.Mutex
	configured bool
	fn         func(ctx context.Context, n int) (trialwire.Response, error)
	states     []string
}

func (a *fakeArm) Configured() bool { return a.configured }

func (a *fakeArm) TraderRaw(ctx context.Context, _ string, state json.RawMessage) (trialwire.Response, time.Duration, int, error) {
	a.mu.Lock()
	a.states = append(a.states, string(state))
	n, fn := len(a.states), a.fn
	a.mu.Unlock()
	resp, err := fn(ctx, n)
	return resp, 10 * time.Millisecond, 4000, err
}

func (a *fakeArm) calls() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.states)
}

func (a *fakeArm) received() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.states...)
}

// okResp is a fully answered response that reports its cost.
func okResp(costUSD float64) trialwire.Response {
	return trialwire.Response{
		Direction: domain.JevDirectionLong, Regime: domain.JevRegimeTrend, EntryQuality: domain.JevEntryQualityGood,
		ToxicFlow: new(0.2), LiquidityStressed: new(0.1), ContinuationProbability: new(0.6), Confidence: new(0.7),
		ModelID: "served-model", Provider: "served-provider", ResponseID: "gen-1", InputTokens: 1200, OutputTokens: 40,
		ReportedCostUSD: &costUSD, Body: `{"model":"served-model"}`,
	}
}

func answer(costUSD float64) func(context.Context, int) (trialwire.Response, error) {
	return func(context.Context, int) (trialwire.Response, error) { return okResp(costUSD), nil }
}

// env is a migrated database with the runner over it and fake arms.
type env struct {
	t        *testing.T
	db       *sql.DB
	store    *repo.Repository
	settings *system.RuntimeSettingsRepository
	trial    *judgement.TrialRepository
	arms     map[string]*fakeArm
	calls    int // how many times the runner asked for an arm's client
	callMu   sync.Mutex
	instID   int64
}

func newEnv(t *testing.T) *env {
	t.Helper()
	conn, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	res, err := conn.Exec(`INSERT INTO instruments (symbol, name, market, is_active) VALUES ('7203', 'x', 'TSE Prime', 1)`)
	if err != nil {
		t.Fatalf("seed instrument: %v", err)
	}
	id, _ := res.LastInsertId()
	return &env{t: t, db: conn, store: repo.New(conn), settings: system.NewRuntimeSettingsRepository(conn),
		trial: judgement.NewTrialRepository(conn), arms: map[string]*fakeArm{}, instID: id}
}

func (e *env) set(key, value string) {
	e.t.Helper()
	if err := e.settings.Set(context.Background(), key, value, now); err != nil {
		e.t.Fatalf("set %s: %v", key, err)
	}
}

func (e *env) enable() { e.set(jevreplaysettings.KeyEnabled, jevreplaysettings.Switch(true)) }

// configure stores the trial arms and gives each a fake client (answering at cost).
func (e *env) configure(cost float64, arms ...jevtrialsettings.Arm) {
	e.t.Helper()
	value, err := jevtrialsettings.EncodeArms(arms)
	if err != nil {
		e.t.Fatalf("EncodeArms: %v", err)
	}
	e.set(jevtrialsettings.KeyArms, value)
	for _, a := range arms {
		e.arms[a.ID] = &fakeArm{configured: true, fn: answer(cost)}
	}
}

func (e *env) setCap(usd float64) {
	e.set(jevtrialsettings.KeyDailyCostCapUSD, strconv.FormatFloat(usd, 'f', -1, 64))
}

func arm(id, cutoff string) jevtrialsettings.Arm {
	return jevtrialsettings.Arm{ID: id, Provider: jevtrialsettings.ProviderOpenRouter, Model: "vendor/" + id, PromptVersion: "trader-v3", LLMCutoff: cutoff}
}

// decide saves a live trader decision at ts with a distinctive saved input and returns its id.
func (e *env) decide(ts time.Time, stateJSON string) int64 {
	e.t.Helper()
	res, err := e.db.Exec(`INSERT INTO jev_decisions (instrument_id, symbol, timestamp, decision_type, state_hash, state_json, question_version, response_json, direction, latency_ms, model_id)
		VALUES (?, '7203', ?, 'trader', 'h', ?, 'trader-v3', '{}', 'LONG', 1, 'm')`, e.instID, sqlutil.FormatTime(ts), stateJSON)
	if err != nil {
		e.t.Fatalf("seed decision: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

// state is a saved input whose key order and unknown key a decode/re-encode would change.
func state(n int) string {
	return `{"zz_future_field":{"n":` + strconv.Itoa(n) + `},"symbol":"7203","price":` + strconv.Itoa(1000+n) + `,"timestamp":"2026-10-10T01:00:00Z"}`
}

// runner builds the runner with the env's clients; paced calls are off.
func (e *env) runner(opts ...service.Option) *service.Runner {
	e.t.Helper()
	callers := func(a jevtrialsettings.Arm) service.Caller {
		e.callMu.Lock()
		e.calls++
		e.callMu.Unlock()
		if f, ok := e.arms[a.ID]; ok {
			return f
		}
		return nil
	}
	r := service.New(e.store, e.trial, e.settings, callers, metalabelsettings.ModePaper, func() time.Time { return now },
		append([]service.Option{service.WithMinInterval(0)}, opts...)...)
	e.t.Cleanup(r.Close)
	return r
}

// start plans and records a pending run for req over the configured arms, as the MCP's start does.
func (e *env) start(req service.Request) (jevreplay.Run, service.Plan) {
	e.t.Helper()
	cfg := jevtrialsettings.Load(context.Background(), e.settings)
	in, err := req.Resolve(cfg.Arms)
	if err != nil {
		e.t.Fatalf("Resolve: %v", err)
	}
	plan, err := service.BuildPlan(context.Background(), e.store, in)
	if err != nil {
		e.t.Fatalf("BuildPlan: %v", err)
	}
	run, err := plan.NewRun("test")
	if err != nil {
		e.t.Fatalf("NewRun: %v", err)
	}
	id, err := repo.InsertRun(context.Background(), e.db, run, now)
	if err != nil {
		e.t.Fatalf("InsertRun: %v", err)
	}
	got, err := e.store.Get(context.Background(), id)
	if err != nil {
		e.t.Fatalf("Get: %v", err)
	}
	return got, plan
}

func (e *env) run(id int64) jevreplay.Run {
	e.t.Helper()
	r, err := e.store.Get(context.Background(), id)
	if err != nil {
		e.t.Fatalf("Get run: %v", err)
	}
	return r
}

func (e *env) count(table string) int {
	e.t.Helper()
	var n int
	if err := e.db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
		e.t.Fatalf("count %s: %v", table, err)
	}
	return n
}

// wholeOctober is a request over the days the tests' decisions fall in.
func wholeOctober(maxRows int, arms ...string) service.Request {
	return service.Request{
		From: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
		MaxRows: maxRows, Arms: arms,
	}
}

func tick(t *testing.T, r *service.Runner) {
	t.Helper()
	if err := r.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}
}
