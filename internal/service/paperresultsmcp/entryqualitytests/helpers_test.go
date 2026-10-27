// Package entryqualitytests holds the HTTP-level tests of the entry_quality_whatif
// tool (issue #911). They live apart from mcptests, which holds the other read
// tools' tests.
package entryqualitytests

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/entryqualitydiag"
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

func newTestServer(t *testing.T, dbPath string) *testServer {
	t.Helper()
	s := settings.Default()
	s.Enabled = true
	s.APIKey = testAPIKey
	opts := paperresultsmcp.Options{DBPath: dbPath, Settings: s, CapitalJPY: 30_000_000}
	opts.Now = func() time.Time { return testkit.FixedNow() }
	opts.Logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
	srv := paperresultsmcp.New(opts)
	hs := httptest.NewServer(srv.Handler())
	t.Cleanup(func() {
		hs.Close()
		_ = srv.Close()
	})
	return &testServer{Server: srv, http: hs}
}

type response struct {
	status int
	body   []byte
}

func (s *testServer) call(t *testing.T, args any) response {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": "entry_quality_whatif", "arguments": args}})
	req, err := http.NewRequest(http.MethodPost, s.http.URL, bytes.NewReader(body))
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

// data decodes the tool's structuredContent.data.
func (r response) data(t *testing.T) entryqualitydiag.Data {
	t.Helper()
	if r.status != http.StatusOK {
		t.Fatalf("status %d, body %s", r.status, r.body)
	}
	var env struct {
		Result struct {
			StructuredContent struct {
				Data json.RawMessage `json:"data"`
			} `json:"structuredContent"`
		} `json:"result"`
	}
	if err := json.Unmarshal(r.body, &env); err != nil {
		t.Fatalf("decode %s: %v", r.body, err)
	}
	t.Logf("entry_quality_whatif -> %s", env.Result.StructuredContent.Data)
	var d entryqualitydiag.Data
	if err := json.Unmarshal(env.Result.StructuredContent.Data, &d); err != nil {
		t.Fatalf("decode data %s: %v", env.Result.StructuredContent.Data, err)
	}
	return d
}

// period covers every fixture decision (JST 2026-10-15 .. 10-17).
var period = map[string]any{"from": "2026-10-10", "to": "2026-10-18"}

func args(extra map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range period {
		out[k] = v
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}

// answer is one stored Jev answer.
type answer struct {
	quality             string
	continuation, toxic float64
	liquidity           float64
}

// fixture is one LONG/SHORT trader decision and the trade signal the engine
// recorded for it ("" head: no signal row; "pass": a signal without a reason).
type fixture struct {
	id        int
	direction string
	version   string
	response  string // response_json; "" builds it from ans
	ans       answer
	abArm     any
	regime    any
	signal    string // reject_reason of the signal; "-": no signal row; "": a signal that passed
	at        string
}

func (f fixture) responseJSON() string {
	if f.response != "" {
		return f.response
	}
	b, _ := json.Marshal(map[string]any{
		"direction": f.direction, "regime": "TREND", "entry_quality": f.ans.quality,
		"toxic_flow": f.ans.toxic, "liquidity_stressed": f.ans.liquidity, "continuation_probability": f.ans.continuation, "confidence": 0.7,
	})
	return string(b)
}

func insertFixture(t *testing.T, w *sql.DB, f fixture) {
	t.Helper()
	version, at := f.version, f.at
	if version == "" {
		version = "trader-v3"
	}
	if at == "" {
		at = "2026-10-15T01:00:00Z"
	}
	testkit.MustExec(t, w, `INSERT INTO jev_decisions (id, instrument_id, symbol, timestamp, decision_type, state_hash, state_json,
		question_version, response_json, direction, confidence, latency_ms, model_id, ab_arm, market_regime)
		VALUES (?, 1, '7203', ?, 'trader', 'h', '{}', ?, ?, ?, 0.7, 5, 'm', ?, ?)`,
		f.id, testkit.Stored(t, at), version, f.responseJSON(), f.direction, f.abArm, f.regime)
	if f.signal == "-" {
		return
	}
	var reason any
	if f.signal != "" {
		reason = f.signal
	}
	dir, risk := "NONE", false
	if f.signal == "" {
		dir, risk = f.direction, true
	}
	testkit.MustExec(t, w, `INSERT INTO trade_signals (instrument_id, jev_decision_id, symbol, direction, timestamp, policy_version, risk_passed, reject_reason)
		VALUES (1, ?, '7203', ?, ?, 'policy-v2', ?, ?)`, f.id, dir, testkit.Stored(t, at), risk, reason)
}
