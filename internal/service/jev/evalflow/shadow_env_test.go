package evalflow_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/jevshadowsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/service/jev"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/jevtest"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/shadow"
	"github.com/ousiassllc/pitha-trador/internal/service/rag"
)

// candidateMarker appears in the trader-v4 entry_quality Instructions only
// (questions_trader_v4.go): the fake API tells the versions apart by it.
const candidateMarker = "Go through the levels from poor upwards"

// fakeJev answers every Trader call like the API does: live (trader-v3)
// questions get entry_quality poor, candidate (trader-v4) questions get good.
// It records which version each call asked, and can fail or hold the
// candidate's calls.
type fakeJev struct {
	mu         sync.Mutex
	versions   []string
	failShadow bool
	// invalidShadow makes candidate calls answer 200 with an answer set that fails
	// validation: the vendor may bill such a call, but nothing is stored for it.
	invalidShadow bool
	holdShadow    chan struct{} // when non-nil, candidate calls wait for it to close
}

func (f *fakeJev) calls(version string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, v := range f.versions {
		if v == version {
			n++
		}
	}
	return n
}

func (f *fakeJev) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	var req struct {
		Questions map[string]struct {
			Instructions string `json:"instructions"`
		} `json:"questions"`
	}
	_ = json.Unmarshal(body, &req)
	version := "trader-v3"
	if strings.Contains(req.Questions["entry_quality"].Instructions, candidateMarker) {
		version = "trader-v4"
	}
	f.mu.Lock()
	f.versions = append(f.versions, version)
	hold, fail, invalid := f.holdShadow, f.failShadow, f.invalidShadow
	f.mu.Unlock()
	if version == "trader-v4" {
		if hold != nil {
			<-hold
		}
		if fail {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if invalid {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"model":"jev-test","answers":{}}`))
			return
		}
	}
	eq := domain.JevEntryQualityPoor
	if version == "trader-v4" {
		eq = domain.JevEntryQualityGood
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(jevtest.Response("jev-test", map[string]any{
		"direction":                jevtest.Choice(domain.JevDirectionLong, 0.7),
		"regime":                   jevtest.Choice(domain.JevRegimeTrend, 0.8),
		"entry_quality":            jevtest.Choice(eq, 0.8),
		"toxic_flow":               jevtest.Noul(0.2),
		"liquidity_stressed":       jevtest.Noul(0.1),
		"continuation_probability": jevtest.Noul(0.6),
	}))
}

type shadowEnv struct {
	t        *testing.T
	db       *sql.DB
	api      *fakeJev
	client   *jev.Client
	trader   *jev.Trader
	shadow   *shadow.Shadow
	results  *judgement.ShadowRepository
	settings *system.RuntimeSettingsRepository
	instID   int64
	now      time.Time
}

func near(a, b float64) bool { return a-b < 1e-12 && b-a < 1e-12 }

func jsonNumber(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) }

func (e *shadowEnv) clock() time.Time { return e.now }

func newShadowEnv(t *testing.T, mode metalabelsettings.Mode, api *fakeJev) *shadowEnv {
	t.Helper()
	server := httptest.NewServer(api)
	t.Cleanup(server.Close)
	db := newTestDB(t)
	e := &shadowEnv{t: t, db: db, api: api, now: time.Date(2026, 10, 22, 1, 0, 0, 0, time.UTC),
		results: judgement.NewShadowRepository(db), settings: system.NewRuntimeSettingsRepository(db)}
	decisions := judgement.NewDecisionRepository(db)
	e.instID = mustCreateInstrument(t, market.NewInstrumentRepository(db), "7203").ID
	client := jev.NewClient(jev.Config{BaseURL: server.URL, MaxAttempts: 1})
	e.client = client
	e.shadow = shadow.New(client, e.results, e.settings, mode, e.clock)
	e.trader = jev.NewTrader(client, decisions, rag.NewService(db, decisions, market.NewSnapshotRepository(db)),
		jev.WithPromptSettings(e.settings), jev.WithShadow(e.shadow))
	return e
}

func (e *shadowEnv) set(key, value string) {
	e.t.Helper()
	if err := e.settings.Set(context.Background(), key, value, e.now); err != nil {
		e.t.Fatalf("set %s: %v", key, err)
	}
}

// enable turns the shadow on with trader-v4 as the candidate.
func (e *shadowEnv) enable() {
	e.set(jevshadowsettings.KeyEnabled, "1")
	e.set(jevshadowsettings.KeyCandidateVersion, `"trader-v4"`)
}

func (e *shadowEnv) evaluate() domain.JevDecision {
	e.t.Helper()
	state := jev.ScoutState{Symbol: "7203", Timestamp: e.now, Price: 2100}
	d, err := e.trader.Evaluate(context.Background(), e.instID, state)
	if err != nil {
		e.t.Fatalf("Evaluate: %v", err)
	}
	return d
}

func (e *shadowEnv) count(query string, args ...any) int {
	e.t.Helper()
	var n int
	if err := e.db.QueryRow(query, args...).Scan(&n); err != nil {
		e.t.Fatalf("%s: %v", query, err)
	}
	return n
}
