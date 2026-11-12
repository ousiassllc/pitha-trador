package trialflow_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/jevtrialsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/domain/jevtrial"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/service/jev"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/jevtest"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/shadow"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/trial"
	"github.com/ousiassllc/pitha-trador/internal/service/rag"
)

type recordingSubmitter struct{ submits, closes int }

func (r *recordingSubmitter) Submit(domain.JevDecision, jev.ScoutState, rag.Context) { r.submits++ }
func (r *recordingSubmitter) Close()                                                 { r.closes++ }

func TestComposeFansOutSkipsNilAndClosesAll(t *testing.T) {
	a, b := &recordingSubmitter{}, &recordingSubmitter{}
	var nilTrial *trial.Trial
	c := trial.Compose(a, nil, b)
	c.Submit(domain.JevDecision{ID: 1}, jev.ScoutState{}, rag.Context{})
	c.Submit(domain.JevDecision{ID: 2}, jev.ScoutState{}, rag.Context{})
	c.Close()
	if a.submits != 2 || b.submits != 2 || a.closes != 1 || b.closes != 1 {
		t.Fatalf("a=%+v b=%+v, want 2 submits and 1 close each", *a, *b)
	}
	if trial.Compose() != nil || trial.Compose(nil, nil) != nil {
		t.Error("Compose of nothing must be nil (no shadow seam)")
	}
	// A nil *Trial (typed nil) is safe on both methods.
	nilTrial.Submit(domain.JevDecision{}, jev.ScoutState{}, rag.Context{})
	nilTrial.Close()
}

// apiRecorder is an OpenRouter stand-in that records what the trial sent.
type apiRecorder struct {
	mu      sync.Mutex
	bodies  []string
	headers []http.Header
	handler http.Handler
}

func (a *apiRecorder) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	a.mu.Lock()
	a.bodies, a.headers = append(a.bodies, string(body)), append(a.headers, r.Header.Clone())
	a.mu.Unlock()
	a.handler.ServeHTTP(w, r)
}

func (a *apiRecorder) requests() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.bodies)
}

// appTrader builds a Trader with trial.TraderOptions (the application's
// wiring) against a live Jev stub and an OpenRouter stub.
func appTrader(t *testing.T, keys trial.Keys, api *apiRecorder) (*jev.Trader, *system.RuntimeSettingsRepository, func() int, int64, *judgement.TrialRepository) {
	t.Helper()
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha_test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	live := httptest.NewServer(jevtest.TraderHandler(jev.TraderResponse{Direction: domain.JevDirectionNone, Confidence: 0.5}))
	t.Cleanup(live.Close)
	if keys.BaseURL == "" && api != nil {
		server := httptest.NewServer(api)
		t.Cleanup(server.Close)
		keys.BaseURL = server.URL
	}
	settings := system.NewRuntimeSettingsRepository(db)
	decisions := judgement.NewDecisionRepository(db)
	inst := mustInstrument(t, market.NewInstrumentRepository(db))
	client := jev.NewClient(jev.Config{BaseURL: live.URL, MaxAttempts: 1})
	opts := trial.TraderOptions(db, client, settings, nil, keys)
	if len(opts) != 3 || len(shadow.TraderOptions(db, client, settings, nil)) != 3 {
		t.Fatalf("option counts: trial %d, shadow %d, want 3 each", len(opts), len(shadow.TraderOptions(db, client, settings, nil)))
	}
	tr := jev.NewTrader(client, decisions, rag.NewService(db, decisions, market.NewSnapshotRepository(db)), opts...)
	t.Cleanup(tr.Close)
	rows := func() int {
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM jev_trial_decisions`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	return tr, settings, rows, inst.ID, judgement.NewTrialRepository(db)
}

func TestAppWiring_NoKeyMeansNoCallAndTheAppStillRuns(t *testing.T) {
	api := &apiRecorder{handler: jevtest.TraderHandler(jev.TraderResponse{})}
	tr, settings, rows, instID, _ := appTrader(t, trial.Keys{}, api)
	// Enabled with an arm, but OPENROUTER_API_KEY is unset: nothing is sent.
	enableApp(t, settings, jevtrialsettings.Arm{ID: "clef", Provider: "openrouter", Model: "cloudflare/clef", PromptVersion: "trader-v3"})
	if _, err := tr.Evaluate(context.Background(), instID, state(time.Now())); err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	tr.Close()
	if api.requests() != 0 || rows() != 0 {
		t.Fatalf("requests=%d rows=%d, want 0 without OPENROUTER_API_KEY", api.requests(), rows())
	}
}

func TestAppWiring_ArmGetsItsOwnKeyModelAndRoutingPreference(t *testing.T) {
	api := &apiRecorder{handler: jevtest.TraderHandler(jev.TraderResponse{Direction: domain.JevDirectionLong, ModelID: "served"})}
	tr, settings, rows, instID, results := appTrader(t, trial.Keys{APIKey: "sk-test"}, api)
	enableApp(t, settings, jevtrialsettings.Arm{ID: "clef", Provider: "openrouter", Model: "cloudflare/clef", PromptVersion: "trader-v3",
		ProviderOnly: []string{"deepinfra"}, ZDR: true})
	if _, err := tr.Evaluate(context.Background(), instID, state(time.Now())); err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	waitFor(t, "the arm's row", func() bool { return rows() == 1 })
	if api.requests() != 1 || rows() != 1 {
		t.Fatalf("requests=%d rows=%d, want 1 each", api.requests(), rows())
	}
	if got := api.headers[0].Get("Authorization"); got != "Bearer sk-test" {
		t.Errorf("Authorization header = %q", got)
	}
	for _, want := range []string{`"model":"cloudflare/clef"`, `"only":["deepinfra"]`, `"allow_fallbacks":false`, `"data_collection":"deny"`, `"zdr":true`} {
		if !strings.Contains(api.bodies[0], want) {
			t.Errorf("request body lacks %s: %s", want, api.bodies[0])
		}
	}
	d, err := results.Get(context.Background(), 1)
	if err != nil || d.Status != jevtrial.StatusOK || d.ArmID != "clef" || d.CostSource != jevtrial.CostUnknown || d.CostUSD != nil {
		t.Fatalf("row = %+v (%v)", d, err)
	}
}

func enableApp(t *testing.T, settings *system.RuntimeSettingsRepository, arm jevtrialsettings.Arm) {
	t.Helper()
	value, err := jevtrialsettings.EncodeArms([]jevtrialsettings.Arm{arm})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for k, v := range map[string]string{jevtrialsettings.KeyArms: value, jevtrialsettings.KeyEnabled: "1"} {
		if err := settings.Set(context.Background(), k, v, now); err != nil {
			t.Fatal(err)
		}
	}
}

// waitFor polls cond until it holds or a generous bound passes: the trial
// runs in goroutines the Trader gives no Wait for.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for !cond() {
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for %s", what)
		case <-time.After(10 * time.Millisecond):
		}
	}
}
