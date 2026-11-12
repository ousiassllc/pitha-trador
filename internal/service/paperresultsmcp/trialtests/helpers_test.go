// Package trialtests holds the HTTP-level tests of the model-comparison trial
// tools (issue #1060): jev_trial_compare and jev_trial_cost. They live apart
// from mcptests, which holds the older read tools' tests.
package trialtests

import (
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain/jevtrial"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/settings"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

const testAPIKey = "TEST-API-KEY-do-not-leak-0123456789"

// Sentinels planted in the columns the tools must never return.
const (
	sentinelBody  = "SENTINEL_TRIAL_RESPONSE_BODY"
	sentinelError = "SENTINEL_TRIAL_ERROR_EXCERPT"
)

func TestMain(m *testing.M) {
	code := m.Run()
	testkit.RemoveTemplate()
	os.Exit(code)
}

type testServer struct {
	*paperresultsmcp.Server
	http *httptest.Server
}

type response struct {
	status int
	body   []byte
}

func newServer(t *testing.T) (*testServer, *sql.DB) {
	t.Helper()
	path, w := testkit.NewMigratedDB(t)
	s := settings.Default()
	s.Enabled = true
	s.APIKey = testAPIKey
	srv := paperresultsmcp.New(paperresultsmcp.Options{
		Settings: s, DBPath: path, Logger: slog.New(slog.NewJSONHandler(&strings.Builder{}, nil)),
		Now: testkit.FixedNow, CapitalJPY: 30_000_000,
	})
	hs := httptest.NewServer(srv.Handler())
	t.Cleanup(func() {
		hs.Close()
		_ = srv.Close()
	})
	return &testServer{Server: srv, http: hs}, w
}

func (s *testServer) post(t *testing.T, method string, params any) response {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	req, err := http.NewRequest(http.MethodPost, s.http.URL, strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+testAPIKey)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response{status: res.StatusCode, body: b}
}

func (s *testServer) call(t *testing.T, name string, args any) response {
	t.Helper()
	return s.post(t, "tools/call", map[string]any{"name": name, "arguments": args})
}

// data decodes a successful tool response's structuredContent.data into out.
func (r response) data(t *testing.T, tool string, out any) {
	t.Helper()
	if r.status != http.StatusOK {
		t.Fatalf("%s: status %d, body %s", tool, r.status, r.body)
	}
	var env struct {
		Result struct {
			StructuredContent struct {
				Tool string          `json:"tool"`
				Data json.RawMessage `json:"data"`
			} `json:"structuredContent"`
			IsError bool `json:"isError"`
		} `json:"result"`
	}
	if err := json.Unmarshal(r.body, &env); err != nil {
		t.Fatalf("%s: decode %s: %v", tool, r.body, err)
	}
	if env.Result.StructuredContent.Tool != tool || env.Result.IsError {
		t.Fatalf("%s: bad envelope %s", tool, r.body)
	}
	if err := json.Unmarshal(env.Result.StructuredContent.Data, out); err != nil {
		t.Fatalf("%s: decode data: %v", tool, err)
	}
}

func (r response) wantError(t *testing.T, status int, code paperresultsmcp.Code) {
	t.Helper()
	var e struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(r.body, &e); err != nil {
		t.Fatalf("decode error body %s: %v", r.body, err)
	}
	if r.status != status || e.Code != string(code) {
		t.Fatalf("want %d %s, got %d %s (%s)", status, code, r.status, e.Code, r.body)
	}
}

// liveDecision inserts a live trader decision at ts (RFC3339 UTC).
func liveDecision(t *testing.T, w *sql.DB, id int, ts, direction string) {
	t.Helper()
	testkit.MustExec(t, w, `INSERT INTO jev_decisions (id, instrument_id, symbol, timestamp, decision_type, state_hash, state_json,
		question_version, response_json, direction, confidence, latency_ms, model_id)
		VALUES (?, 1, '7203', ?, 'trader', 'h', '{}', 'trader-v3', '{}', ?, 0.7, 5, 'm')`, id, testkit.Stored(t, ts), direction)
}

// row is one jev_trial_decisions row to insert; zero values mean "ok, LONG, fair".
type row struct {
	decision int
	arm      string
	status   string
	failure  string
	dir      string // answer direction (ok and partial rows)
	eq       string
	trunc    bool
	rag      bool
	cutoff   string
	created  string
	cost     *float64
	source   string
	counted  float64
}

func f64(v float64) *float64 { return &v }

func insertRow(t *testing.T, w *sql.DB, r row) {
	t.Helper()
	d := jevtrial.Decision{
		JevDecisionID: int64(r.decision), ArmID: r.arm, PromptVersion: "trader-v3", Provider: "openrouter", RequestedModel: "vendor/" + r.arm,
		Status: orDefault(r.status, jevtrial.StatusOK), FailureCode: r.failure, ErrorExcerpt: sentinelError,
		CostUSD: r.cost, CostSource: orDefault(r.source, jevtrial.CostUnknown), CountedCostUSD: r.counted,
		TruncationSuspected: r.trunc, RAGIncludesLiveModelAnswers: r.rag, LLMCutoffStatus: r.cutoff,
	}
	body := `{"note":"` + sentinelBody + `"}`
	d.ResponseJSON = &body
	if d.Status == jevtrial.StatusOK || d.Status == jevtrial.StatusPartial {
		dir, eq, regime := orDefault(r.dir, "LONG"), orDefault(r.eq, "fair"), "TREND"
		d.Direction, d.EntryQuality, d.Regime = &dir, &eq, &regime
		d.ToxicFlow, d.LiquidityStressed, d.ContinuationProbability = f64(0.1), f64(0.1), f64(0.5)
	}
	if r.created != "" {
		ts, err := judgementTime(r.created)
		if err != nil {
			t.Fatal(err)
		}
		d.CreatedAt = ts
	}
	if _, err := judgement.NewTrialRepository(w).Insert(t.Context(), d); err != nil {
		t.Fatal(err)
	}
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// outcome inserts a calibration_outcomes row (the LIVE direction's label).
func outcome(t *testing.T, w *sql.DB, decisionID, horizon, labelVersion int, futureReturn float64) {
	t.Helper()
	testkit.MustExec(t, w, `INSERT INTO calibration_outcomes (jev_decision_id, horizon_minutes, future_return,
		max_adverse_excursion, max_favorable_excursion, was_direction_correct, barrier_hit, net_return_bps,
		entry_ref_price, label_version) VALUES (?, ?, ?, 0, 0, 1, 'tp', 10, 100, ?)`, decisionID, horizon, futureReturn, labelVersion)
}

// counterfactual inserts a counterfactual_labels row (an existing record; the tool computes none).
func counterfactual(t *testing.T, w *sql.DB, decisionID, horizon int, barrier string, netBps float64) {
	t.Helper()
	testkit.MustExec(t, w, `INSERT INTO counterfactual_labels (jev_decision_id, horizon_minutes, barrier_hit, net_return_bps,
		entry_ref_price, label_version, future_return, max_adverse_excursion, max_favorable_excursion, llm_cutoff, created_at)
		VALUES (?, ?, ?, ?, 100, 2, 0, 0, 0, '2025-01-01', ?)`, decisionID, horizon, barrier, netBps, testkit.Stored(t, "2026-10-02T00:00:00Z"))
}

var period = map[string]any{"from": "2026-10-01", "to": "2026-10-10"}

func judgementTime(rfc3339 string) (time.Time, error) { return time.Parse(time.RFC3339, rfc3339) }
