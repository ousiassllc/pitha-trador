package codexflow_test

import (
	"context"
	"database/sql"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/jevtrialsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/domain/jevtrial"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/service/jev"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/codexwire"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/jevtest"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/trial"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/trialwire"
	"github.com/ousiassllc/pitha-trador/internal/service/rag"
)

type callFn = func(ctx context.Context, req jev.TraderRequest) (trialwire.Response, time.Duration, int, error)

// fakeArm stands in for codexwire.Client (and trialwire.Client): fn answers each
// call and login is what it reports ("" is logged in with ChatGPT).
type fakeArm struct {
	mu         sync.Mutex
	configured bool
	login      string
	fn         callFn
	n          int
}

func (a *fakeArm) Configured() bool { return a.configured }

func (a *fakeArm) Login(context.Context) (string, time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.login == "" {
		return jevtrialsettings.CodexLoginChatGPT, time.Date(2026, 11, 13, 1, 0, 0, 0, time.UTC)
	}
	return a.login, time.Date(2026, 11, 13, 1, 0, 0, 0, time.UTC)
}

func (a *fakeArm) Trader(ctx context.Context, req jev.TraderRequest) (trialwire.Response, time.Duration, int, error) {
	a.mu.Lock()
	a.n++
	fn := a.fn
	a.mu.Unlock()
	return fn(ctx, req)
}

func (a *fakeArm) calls() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.n
}

func okResp() trialwire.Response {
	return trialwire.Response{
		Direction: domain.JevDirectionLong, Regime: domain.JevRegimeTrend, EntryQuality: domain.JevEntryQualityGood,
		ToxicFlow: new(0.2), LiquidityStressed: new(0.1), ContinuationProbability: new(0.6), Confidence: new(0.7),
		ModelID: "served-model", Provider: "served-provider", ResponseID: "gen-1", InputTokens: 1200, OutputTokens: 40,
		Body: `{"model":"served-model"}`,
	}
}

func answer(resp trialwire.Response, bytes int) callFn {
	return func(context.Context, jev.TraderRequest) (trialwire.Response, time.Duration, int, error) {
		return resp, 80 * time.Millisecond, bytes, nil
	}
}

func failedCall(err error) callFn {
	return func(context.Context, jev.TraderRequest) (trialwire.Response, time.Duration, int, error) {
		return trialwire.Response{}, 30 * time.Millisecond, 2000, err
	}
}

// env is a live jev.Trader (real database, RAG, httptest live Jev) with the trial
// on its shadow seam and fake arms, on a clock the test moves.
type env struct {
	t        *testing.T
	db       *sql.DB
	trader   *jev.Trader
	trial    *trial.Trial
	settings *system.RuntimeSettingsRepository
	results  *judgement.TrialRepository
	arms     map[string]*fakeArm
	instID   int64

	mu  sync.Mutex
	now time.Time
}

func (e *env) clock() time.Time {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.now
}

func (e *env) advance(d time.Duration) {
	e.mu.Lock()
	e.now = e.now.Add(d)
	e.mu.Unlock()
}

func newEnv(t *testing.T) *env {
	t.Helper()
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha_test.db"))
	if err != nil {
		t.Fatalf("sqlitedb.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	e := &env{t: t, db: db, arms: map[string]*fakeArm{}, results: judgement.NewTrialRepository(db),
		now: time.Date(2026, 11, 13, 1, 0, 0, 0, time.UTC)} // 10:00 JST
	e.settings = system.NewRuntimeSettingsRepository(db)
	server := httptest.NewServer(jevtest.TraderHandler(jev.TraderResponse{
		Direction: domain.JevDirectionNone, EntryQuality: domain.JevEntryQualityPoor, Confidence: 0.5, ModelID: "jev-live"}))
	t.Cleanup(server.Close)
	client := jev.NewClient(jev.Config{BaseURL: server.URL, MaxAttempts: 1, Now: e.clock})
	callers := func(arm jevtrialsettings.Arm) trial.Caller {
		if a, ok := e.arms[arm.ID]; ok {
			return a
		}
		return nil
	}
	e.trial = trial.New(e.results, e.settings, callers, metalabelsettings.ModePaper, e.clock, trial.WithMinInterval(0))
	decisions := judgement.NewDecisionRepository(db)
	inst, err := market.NewInstrumentRepository(db).Create(context.Background(), domain.Instrument{Symbol: "7203", Name: "Toyota", Market: "TSE Prime", IsActive: true})
	if err != nil {
		t.Fatalf("create instrument: %v", err)
	}
	e.instID = inst.ID
	e.trader = jev.NewTrader(client, decisions, rag.NewService(db, decisions, market.NewSnapshotRepository(db)),
		jev.WithPromptSettings(e.settings), jev.WithShadow(e.trial))
	return e
}

// addArm registers a fake arm (configured) answering with fn.
func (e *env) addArm(id string, fn callFn) *fakeArm {
	a := &fakeArm{configured: true, fn: fn}
	e.arms[id] = a
	return a
}

func defaultArm(id string) jevtrialsettings.Arm {
	return jevtrialsettings.Arm{ID: id, Provider: jevtrialsettings.ProviderOpenRouter, Model: "cloudflare/" + id,
		PromptVersion: "trader-v3", ListPriceUSDPerMtok: 0.038}
}

// enable turns the trial on with arms.
func (e *env) enable(arms ...jevtrialsettings.Arm) {
	e.t.Helper()
	value, err := jevtrialsettings.EncodeArms(arms)
	if err != nil {
		e.t.Fatalf("encode arms: %v", err)
	}
	e.set(jevtrialsettings.KeyArms, value)
	e.set(jevtrialsettings.KeyEnabled, "1")
}

func (e *env) set(key, value string) {
	e.t.Helper()
	if err := e.settings.Set(context.Background(), key, value, e.clock()); err != nil {
		e.t.Fatalf("set %s: %v", key, err)
	}
}

func (e *env) get(key string) (string, bool) {
	e.t.Helper()
	v, ok, err := e.settings.Get(context.Background(), key)
	if err != nil {
		e.t.Fatalf("get %s: %v", key, err)
	}
	return v, ok
}

func state(now time.Time) jev.ScoutState {
	return jev.ScoutState{Symbol: "7203", Timestamp: now, Price: 2100}
}

// evaluate makes one live decision (which hands it to the trial) and waits for
// the trial's comparisons to finish.
func (e *env) evaluate() {
	e.t.Helper()
	if _, err := e.trader.Evaluate(context.Background(), e.instID, state(e.clock())); err != nil {
		e.t.Fatalf("Evaluate: %v", err)
	}
	e.trial.Wait()
}

func (e *env) count(query string, args ...any) int {
	e.t.Helper()
	var n int
	if err := e.db.QueryRow(query, args...).Scan(&n); err != nil {
		e.t.Fatalf("%s: %v", query, err)
	}
	return n
}

// rows returns every trial row, oldest first.
func (e *env) rows() []jevtrial.Decision {
	e.t.Helper()
	ids, err := e.db.Query(`SELECT id FROM jev_trial_decisions ORDER BY id`)
	if err != nil {
		e.t.Fatal(err)
	}
	defer func() { _ = ids.Close() }()
	var out []jevtrial.Decision
	for ids.Next() {
		var id int64
		if err := ids.Scan(&id); err != nil {
			e.t.Fatal(err)
		}
		d, err := e.results.Get(context.Background(), id)
		if err != nil {
			e.t.Fatal(err)
		}
		out = append(out, d)
	}
	if err := ids.Err(); err != nil {
		e.t.Fatal(err)
	}
	return out
}

// rowOf returns the only row of arm.
func (e *env) rowOf(arm string) jevtrial.Decision {
	e.t.Helper()
	var found []jevtrial.Decision
	for _, d := range e.rows() {
		if d.ArmID == arm {
			found = append(found, d)
		}
	}
	if len(found) != 1 {
		e.t.Fatalf("rows of arm %s = %d, want 1", arm, len(found))
	}
	return found[0]
}

// --- Codex helpers ---

func codexArm(id string) jevtrialsettings.Arm {
	return jevtrialsettings.Arm{ID: id, Provider: jevtrialsettings.ProviderCodexChatGPT, Model: "gpt-6.1-sol", PromptVersion: "trader-v3"}
}

func codexResp() trialwire.Response {
	r := okResp()
	r.ModelID, r.Provider, r.ResponseID = "gpt-6.1-sol", codexwire.ServedProvider, "thread-1"
	r.InputTokens, r.OutputTokens = 1000, 100
	return r
}

// enableCodex turns the Codex provider on (it is off by default).
func (e *env) enableCodex(calls, tokens string) {
	e.t.Helper()
	e.set(jevtrialsettings.KeyCodexEnabled, "1")
	if calls != "" {
		e.set(jevtrialsettings.KeyCodexDailyCallCap, calls)
	}
	if tokens != "" {
		e.set(jevtrialsettings.KeyCodexDailyTokenCap, tokens)
	}
}

func (e *env) codexHalt() *jevtrialsettings.CodexHalt {
	e.t.Helper()
	raw, ok := e.get(jevtrialsettings.KeyCodexHalt)
	if !ok {
		return nil
	}
	h, err := jevtrialsettings.ParseCodexHalt(raw)
	if err != nil {
		e.t.Fatal(err)
	}
	return &h
}

func (e *env) skips(arm, reason string) int {
	return e.count(`SELECT COUNT(*) FROM jev_trial_skips WHERE arm_id = ? AND reason = ?`, arm, reason)
}
