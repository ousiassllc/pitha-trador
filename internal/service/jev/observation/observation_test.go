package observation_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/config/watchsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/jobqueue"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/service/jev"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/jevtest"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/observation"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/shadow"
	"github.com/ousiassllc/pitha-trador/internal/service/rag"
)

var barAt = time.Date(2026, 9, 27, 9, 31, 0, 0, time.UTC)

// rig is a Scout over a real database and a fake Jev server, with an
// observation gate over the same runtime_settings table.
type rig struct {
	scout    *jev.Scout
	jobs     *jobqueue.JobRepository
	settings *system.RuntimeSettingsRepository
	insts    map[string]domain.Instrument
	calls    *atomic.Int32
	db       *sql.DB
	gate     *observation.Gate
	held     *fakeHeld
}

func newRig(t *testing.T, withGate bool, symbols ...string) *rig {
	t.Helper()
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha_test.db"))
	if err != nil {
		t.Fatalf("sqlitedb.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	var calls atomic.Int32
	passing := jevtest.ScoutHandler(jev.ScoutResponse{InterestingNow: 0.9, LiquidityOk: 0.9, AbnormalActivity: 0.9})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		passing(w, r)
	}))
	t.Cleanup(server.Close)

	snapshots := market.NewSnapshotRepository(db)
	instruments := market.NewInstrumentRepository(db)
	r := &rig{held: &fakeHeld{}, jobs: jobqueue.NewJobRepository(db), settings: system.NewRuntimeSettingsRepository(db), insts: map[string]domain.Instrument{}, calls: &calls, db: db}
	for _, sym := range symbols {
		inst, err := instruments.Create(context.Background(), domain.Instrument{Symbol: sym, Name: sym + " Inc", Market: "TSE Prime", IsActive: true})
		if err != nil {
			t.Fatalf("create instrument %q: %v", sym, err)
		}
		if _, err := snapshots.Insert(context.Background(), domain.Snapshot{
			InstrumentID: inst.ID, Symbol: sym, Timestamp: barAt, Price: 2100, Volume: 1000, Turnover: 2_000_000, RawDataJSON: `{}`,
		}); err != nil {
			t.Fatalf("insert snapshot: %v", err)
		}
		r.insts[sym] = inst
	}
	now := func() time.Time { return barAt.Add(10 * time.Second) }
	opts := []jev.Option{jev.WithClock(now)}
	if withGate {
		r.gate = observation.New(r.settings, r.held, now)
		opts = append(opts, jev.WithObservation(r.gate))
	}
	decisions := judgement.NewDecisionRepository(db)
	thresholds := config.JevScoutConfig{MinInterestingNow: 0.65, MinLiquidityOk: 0.70, MinAbnormalActivity: 0.55}
	r.scout = jev.NewScout(jev.NewClient(jev.Config{BaseURL: server.URL, MaxAttempts: 1}), decisions, snapshots, r.jobs,
		rag.NewService(db, decisions, snapshots), thresholds, opts...)
	return r
}

func (r *rig) set(t *testing.T, key, value string) {
	t.Helper()
	if err := r.settings.Set(context.Background(), key, value, barAt); err != nil {
		t.Fatalf("set %s: %v", key, err)
	}
}

// observe turns the observation on for symbols.
func (r *rig) observe(t *testing.T, symbols string) {
	t.Helper()
	enc, _ := json.Marshal(symbols)
	r.set(t, watchsettings.KeyObservationSymbols, string(enc))
	r.set(t, watchsettings.KeyObservationEnabled, "1")
}

func (r *rig) handle(t *testing.T, symbol string) error {
	t.Helper()
	inst := r.insts[symbol]
	payload, _ := json.Marshal(jev.ScoutJobPayload{InstrumentID: inst.ID, Symbol: symbol})
	job, err := r.jobs.Enqueue(context.Background(), jobqueue.JobQueueJevScout, string(payload), time.Now().UTC())
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	return r.scout.HandleJob(context.Background(), job)
}

// traderJobs drains the jev-trader queue and returns the symbols enqueued for
// Trader (and so for the Policy Engine, an order, Paper and Live).
func (r *rig) traderJobs(t *testing.T) []string {
	t.Helper()
	var out []string
	for {
		job, err := r.jobs.ClaimNext(context.Background(), jobqueue.JobQueueJevTrader, time.Now().UTC())
		if err != nil {
			return out
		}
		var p jev.ScoutJobPayload
		if err := json.Unmarshal([]byte(job.PayloadJSON), &p); err != nil {
			t.Fatalf("decode trader payload: %v", err)
		}
		out = append(out, p.Symbol)
	}
}

// issue #1055: an observation symbol is asked on Jev Scout (the evaluation
// data is the point) but, even when it passes, never gets a jev-trader job: it
// cannot reach the Policy Engine, an order, Paper or Live.
func TestScout_ObservationSymbolCallsJevButNeverReachesTrader(t *testing.T) {
	r := newRig(t, true, "7203", "6758")
	r.observe(t, "7203")

	if err := r.handle(t, "7203"); err != nil {
		t.Fatalf("HandleJob(observation symbol) = %v", err)
	}
	if got := r.calls.Load(); got != 1 {
		t.Errorf("Jev Scout calls = %d, want 1", got)
	}
	if got := r.traderJobs(t); len(got) != 0 {
		t.Errorf("jev-trader jobs = %v for an observation symbol, want none", got)
	}

	// The same passing answer for an ordinary symbol goes on to Trader as before.
	if err := r.handle(t, "6758"); err != nil {
		t.Fatalf("HandleJob(ordinary symbol) = %v", err)
	}
	if got := r.traderJobs(t); len(got) != 1 || got[0] != "6758" {
		t.Errorf("jev-trader jobs = %v, want [6758]", got)
	}
}

// An observation call's answer stays out of the similar-case index the live
// Trader prompt searches (jev_decision_vectors): only an ordinary symbol's
// Scout decision is indexed.
func TestScout_ObservationDecisionIsNotIndexedForRAG(t *testing.T) {
	r := newRig(t, true, "7203", "6758")
	r.observe(t, "7203")
	vectors := func() int {
		var n int
		if err := r.db.QueryRow(`SELECT COUNT(*) FROM jev_decision_vectors`).Scan(&n); err != nil {
			t.Fatalf("count jev_decision_vectors: %v", err)
		}
		return n
	}

	if err := r.handle(t, "7203"); err != nil {
		t.Fatalf("HandleJob(observation symbol) = %v", err)
	}
	if got := vectors(); got != 0 {
		t.Errorf("jev_decision_vectors = %d after an observation call, want 0", got)
	}
	if err := r.handle(t, "6758"); err != nil {
		t.Fatalf("HandleJob(ordinary symbol) = %v", err)
	}
	if got := vectors(); got != 1 {
		t.Errorf("jev_decision_vectors = %d after an ordinary call, want 1", got)
	}
}

// The default is off: a listed symbol is an ordinary one while the switch is
// off, and a Scout built without a gate behaves exactly as before.
func TestScout_ObservationIsOffByDefault(t *testing.T) {
	for name, withGate := range map[string]bool{"gate, nothing stored": true, "no gate": false} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t, withGate, "7203")
			if withGate { // listed but the switch is off
				enc, _ := json.Marshal("7203")
				r.set(t, watchsettings.KeyObservationSymbols, string(enc))
			}
			if err := r.handle(t, "7203"); err != nil {
				t.Fatalf("HandleJob = %v", err)
			}
			if got := r.traderJobs(t); len(got) != 1 {
				t.Errorf("jev-trader jobs = %v, want the ordinary flow's one", got)
			}
		})
	}
}

// The cap stops ONLY the observation calls: once it is reached an observation
// job is skipped without a Jev call, while the ordinary symbol's Scout call and
// its jev-trader job go on (the normal trading path is never gated).
func TestScout_CostCapStopsOnlyTheObservationCalls(t *testing.T) {
	r := newRig(t, true, "7203", "6758")
	r.observe(t, "7203")
	r.set(t, watchsettings.KeyObservationDailyCostCapUSD, "0.0000001")

	// The first call is admitted (nothing spent yet) and spends more than the cap.
	if err := r.handle(t, "7203"); err != nil {
		t.Fatalf("first observation call = %v", err)
	}
	if r.calls.Load() != 1 {
		t.Fatalf("Jev calls = %d, want 1", r.calls.Load())
	}

	var skipped *jobqueue.SkippedError
	if err := r.handle(t, "7203"); !errors.As(err, &skipped) || skipped.Reason != jev.ObservationCapSkip {
		t.Fatalf("observation call over the cap = %v, want the cap skip", err)
	}
	if r.calls.Load() != 1 {
		t.Errorf("Jev calls = %d after the cap, want no further observation call", r.calls.Load())
	}

	for range 3 { // the normal trading path keeps running, call after call
		if err := r.handle(t, "6758"); err != nil {
			t.Fatalf("ordinary symbol over the observation cap = %v, want it to run", err)
		}
	}
	if r.calls.Load() != 4 {
		t.Errorf("Jev calls = %d, want 1 observation + 3 ordinary", r.calls.Load())
	}
	if got := r.traderJobs(t); len(got) != 3 {
		t.Errorf("jev-trader jobs = %v, want the 3 ordinary symbols' jobs", got)
	}
}

// A new JST day starts from zero again.
func TestGate_CapResetsOnTheNextJSTDay(t *testing.T) {
	r := newRig(t, true, "7203")
	r.observe(t, "7203")
	ctx := context.Background()
	if err := r.gate.RecordCall(ctx, jev.ScoutResponse{InputTokens: 100}); err != nil {
		t.Fatal(err)
	}
	r.set(t, watchsettings.KeyObservationDailyCostCapUSD, "0.000001") // below the one call's cost
	if ok, _, err := r.gate.Admit(ctx); err != nil || ok {
		t.Fatalf("Admit over the cap = %v, %v; want refused", ok, err)
	}
	tomorrow := observation.New(r.settings, r.held, func() time.Time { return barAt.Add(24 * time.Hour) })
	if ok, _, err := tomorrow.Admit(ctx); err != nil || !ok {
		t.Errorf("Admit on the next day = %v, %v; want admitted", ok, err)
	}
}

func TestGate_CostIsReportedOrPricedFromUsageNeverFree(t *testing.T) {
	ctx := context.Background()
	for name, tc := range map[string]struct {
		resp jev.ScoutResponse
		want float64
	}{
		"reported cost":     {jev.ScoutResponse{RequestCost: ptr(0.02)}, 0.02},
		"input tokens":      {jev.ScoutResponse{InputTokens: 1_000_000}, shadow.InputPriceUSDPerMtok},
		"estimated tokens":  {jev.ScoutResponse{RequestBytes: 2_000_000}, shadow.InputPriceUSDPerMtok},
		"no usage at all":   {jev.ScoutResponse{}, shadow.InputCostUSD(1)},
		"negative reported": {jev.ScoutResponse{RequestCost: ptr(-1), InputTokens: 1_000_000}, shadow.InputPriceUSDPerMtok},
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t, true)
			if err := r.gate.RecordCall(ctx, tc.resp); err != nil {
				t.Fatal(err)
			}
			raw, _, _ := r.settings.Get(ctx, watchsettings.KeyObservationUsage)
			u, err := watchsettings.ParseUsage(raw)
			if err != nil || u.Calls != 1 || u.CostUSD != tc.want {
				t.Errorf("usage = %+v, %v; want 1 call costing %v", u, err, tc.want)
			}
		})
	}
}

func TestGate_FailureCapStopsTheDay(t *testing.T) {
	r := newRig(t, true)
	ctx := context.Background()
	for i := range watchsettings.DailyFailureCap {
		if ok, _, err := r.gate.Admit(ctx); err != nil || !ok {
			t.Fatalf("Admit before failure %d = %v, %v", i, ok, err)
		}
		if err := r.gate.RecordFailure(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if ok, reason, err := r.gate.Admit(ctx); err != nil || ok || reason == "" {
		t.Errorf("Admit after %d failures = %v, %q, %v; want refused with a reason", watchsettings.DailyFailureCap, ok, reason, err)
	}
}

func TestGate_ObservedFollowsTheSettings(t *testing.T) {
	r := newRig(t, true)
	ctx := context.Background()
	if ok, err := r.gate.Observed(ctx, "7203"); err != nil || ok {
		t.Fatalf("Observed with nothing stored = %v, %v; want false", ok, err)
	}
	r.observe(t, "7203, 6758")
	for sym, want := range map[string]bool{"7203": true, "6758": true, "9984": false} {
		if ok, err := r.gate.Observed(ctx, sym); err != nil || ok != want {
			t.Errorf("Observed(%s) = %v, %v; want %v", sym, ok, err, want)
		}
	}
	r.set(t, watchsettings.KeyObservationEnabled, "0")
	if ok, _ := r.gate.Observed(ctx, "7203"); ok {
		t.Error("Observed is true with the switch off")
	}
}

func ptr(v float64) *float64 { return &v }
