// Package shadowtests holds the HTTP-level tests of the Jev prompt tools (issue
// #872): jev_shadow_compare, jev_shadow_cost and the explicit promotion
// jev_prompt_promote (its write switch, its audit row, how narrow its write
// path is). They live apart from mcptests, which holds the older read tools'
// tests.
package shadowtests

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

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/settings"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

const testAPIKey = "TEST-API-KEY-do-not-leak-0123456789"

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

func enabledSettings() settings.Settings {
	s := settings.Default()
	s.Enabled = true
	s.APIKey = testAPIKey
	return s
}

func newServer(t *testing.T, opts paperresultsmcp.Options) *testServer {
	t.Helper()
	opts.Logger = slog.New(slog.NewJSONHandler(&strings.Builder{}, nil))
	opts.Now = testkit.FixedNow
	if opts.CapitalJPY == 0 {
		opts.CapitalJPY = 30_000_000
	}
	srv := paperresultsmcp.New(opts)
	hs := httptest.NewServer(srv.Handler())
	t.Cleanup(func() {
		hs.Close()
		_ = srv.Close()
	})
	return &testServer{Server: srv, http: hs}
}

// shadowServer is a server over a migrated database plus a separate writer
// handle to seed and inspect it.
func shadowServer(t *testing.T) (*testServer, *sql.DB) {
	t.Helper()
	path, w := testkit.NewMigratedDB(t)
	return newServer(t, paperresultsmcp.Options{Settings: enabledSettings(), DBPath: path}), w
}

func (s *testServer) call(t *testing.T, name string, args any) response {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": name, "arguments": args}})
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
		t.Fatalf("%s: decode data %s: %v", tool, env.Result.StructuredContent.Data, err)
	}
}

type errorBody struct {
	Code  string `json:"code"`
	Error struct {
		Data map[string]string `json:"data"`
	} `json:"error"`
}

func (r response) wantError(t *testing.T, status int, code paperresultsmcp.Code) errorBody {
	t.Helper()
	var e errorBody
	if err := json.Unmarshal(r.body, &e); err != nil {
		t.Fatalf("decode error body %s: %v", r.body, err)
	}
	if r.status != status || e.Code != string(code) {
		t.Fatalf("want %d %s, got %d %s (%s)", status, code, r.status, e.Code, r.body)
	}
	return e
}

func count(t *testing.T, w *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := w.QueryRowContext(t.Context(), query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func storedValue(t *testing.T, w *sql.DB, key string) (string, bool) {
	t.Helper()
	var v string
	err := w.QueryRowContext(t.Context(), `SELECT value FROM runtime_settings WHERE key = ?`, key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", false
	}
	if err != nil {
		t.Fatal(err)
	}
	return v, true
}

// liveDecision inserts a live trader decision in the test period whose
// response_json carries the entry_quality (jev_decisions has no such column).
func liveDecision(t *testing.T, w *sql.DB, id int, ts, version, direction, entryQuality string) {
	t.Helper()
	resp, _ := json.Marshal(map[string]any{"entry_quality": entryQuality, "direction": direction})
	testkit.MustExec(t, w, `INSERT INTO jev_decisions (id, instrument_id, symbol, timestamp, decision_type, state_hash, state_json,
		question_version, response_json, direction, confidence, latency_ms, model_id)
		VALUES (?, 1, '7203', ?, 'trader', 'h', '{}', ?, ?, ?, 0.7, 5, 'm')`, id, testkit.Stored(t, ts), version, string(resp), direction)
}

// shadowDecision inserts the shadow's answer for live decision id.
func shadowDecision(t *testing.T, w *sql.DB, id int, version, direction, entryQuality string, cost float64, createdAt string) {
	t.Helper()
	testkit.MustExec(t, w, `INSERT INTO jev_shadow_decisions (jev_decision_id, prompt_version, model_id, response_json, direction, regime,
		entry_quality, confidence, toxic_flow, liquidity_stressed, continuation_probability, latency_ms, input_tokens, request_cost, created_at)
		VALUES (?, ?, 'jev-test', '{}', ?, 'TREND', ?, 0.6, 0.1, 0.1, 0.5, 100, 1200, ?, ?)`,
		id, version, direction, entryQuality, cost, testkit.Stored(t, createdAt))
}

// outcome inserts a calibration_outcomes row with the given future_return
// (percent) for the LIVE direction's label.
func outcome(t *testing.T, w *sql.DB, decisionID, horizon, labelVersion int, futureReturn float64, correct, barrier, net any) {
	t.Helper()
	testkit.MustExec(t, w, `INSERT INTO calibration_outcomes (jev_decision_id, horizon_minutes, future_return,
		max_adverse_excursion, max_favorable_excursion, was_direction_correct, barrier_hit, net_return_bps,
		entry_ref_price, label_version) VALUES (?, ?, ?, 0, 0, ?, ?, ?, 100, ?)`,
		decisionID, horizon, futureReturn, correct, barrier, net, labelVersion)
}

var period = map[string]any{"from": "2026-10-01", "to": "2026-10-10"}
