// Package disclosureskiptests holds the HTTP-level tests of the
// disclosure_skip_summary tool (issue #935). They live apart from mcptests,
// which holds the other read tools' tests.
package disclosureskiptests

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
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/disclosureskipdiag"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/settings"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

const (
	testAPIKey = "TEST-API-KEY-do-not-leak-0123456789"
	toolName   = "disclosure_skip_summary"
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

func newTestServer(t *testing.T, dbPath string, driverName string) *testServer {
	t.Helper()
	s := settings.Default()
	s.Enabled = true
	s.APIKey = testAPIKey
	opts := paperresultsmcp.Options{DBPath: dbPath, DriverName: driverName, Settings: s, CapitalJPY: 30_000_000}
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
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": toolName, "arguments": args}})
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

// raw returns the tool's structuredContent.data as sent.
func (r response) raw(t *testing.T) json.RawMessage {
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
	t.Logf("%s -> %s", toolName, env.Result.StructuredContent.Data)
	return env.Result.StructuredContent.Data
}

func (r response) data(t *testing.T) disclosureskipdiag.Data {
	t.Helper()
	var d disclosureskipdiag.Data
	if err := json.Unmarshal(r.raw(t), &d); err != nil {
		t.Fatalf("decode data: %v", err)
	}
	return d
}

// period covers every fixture decision (JST 2026-10-15).
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

// decision inserts one trader decision.
func decision(t *testing.T, w *sql.DB, id int, direction, version, at string) {
	t.Helper()
	testkit.MustExec(t, w, `INSERT INTO jev_decisions (id, instrument_id, symbol, timestamp, decision_type, state_hash, state_json,
		question_version, response_json, direction, confidence, latency_ms, model_id)
		VALUES (?, 1, '7203', ?, 'trader', 'h', '{}', ?, '{}', ?, 0.7, 5, 'm')`, id, testkit.Stored(t, at), version, direction)
}

// record inserts the recorder's verdict for decision id; kinds are the kinds of
// its matching disclosures.
func record(t *testing.T, w *sql.DB, id int, at, verdict, reason string, kinds ...string) {
	t.Helper()
	var r any
	if reason != "" {
		r = reason
	}
	testkit.MustExec(t, w, `INSERT INTO disclosure_skip_records (id, jev_decision_id, symbol, decided_at, verdict, unknown_reason, window_from, rule_version, evaluated_at)
		VALUES (?, ?, '7203', ?, ?, ?, ?, 1, ?)`, id, id, testkit.Stored(t, at), verdict, r, testkit.Stored(t, "2026-10-14T06:00:00Z"), testkit.Stored(t, at))
	for _, k := range kinds {
		testkit.MustExec(t, w, `INSERT INTO disclosure_skip_matches (record_id, published_at, title, kind, document_url)
			VALUES (?, ?, 'SENTINEL-TITLE', ?, 'https://example.test/SENTINEL-URL.pdf')`, id, testkit.Stored(t, "2026-10-15T00:30:00Z"), k)
	}
}

const decidedAt = "2026-10-15T01:00:00Z"

// seed writes the period's decisions, the recorder's verdicts and the labels:
//
//	1 LONG  skip (earnings + forecast_revision)   +40
//	2 SHORT skip (forecast_revision)               -20
//	3 LONG  no_skip                                +10 (a v2 row superseded by the newer one)
//	4 LONG  no_skip                                -30
//	5 SHORT no_skip                                 unlabeled
//	6 LONG  unknown no_fetch                        +5
//	7 LONG  unknown incomplete_fetch                v1 label only: unlabeled
//	8 LONG  not recorded                            +1000 (must not be counted anywhere)
//	9 NONE  not an entry candidate, but carries a record and a label
//	10 LONG trader-v4 (another cohort) skip         +900
//	11 LONG outside the period skip                 +900
func seed(t *testing.T) (string, *sql.DB) {
	t.Helper()
	path, w := testkit.NewMigratedDB(t)
	testkit.MustExec(t, w, `INSERT INTO instruments (id, symbol, name, market) VALUES (1, '7203', 'n', 'TSE')`)
	for id, d := range map[int][2]string{
		1: {"LONG", "trader-v3"}, 2: {"SHORT", "trader-v3"}, 3: {"LONG", "trader-v3"}, 4: {"LONG", "trader-v3"}, 5: {"SHORT", "trader-v3"},
		6: {"LONG", "trader-v3"}, 7: {"LONG", "trader-v3"}, 8: {"LONG", "trader-v3"}, 9: {"NONE", "trader-v3"}, 10: {"LONG", "trader-v4"},
	} {
		decision(t, w, id, d[0], d[1], decidedAt)
	}
	decision(t, w, 11, "LONG", "trader-v3", "2026-10-01T01:00:00Z")
	record(t, w, 1, decidedAt, "skip", "", "earnings", "forecast_revision")
	record(t, w, 2, decidedAt, "skip", "", "forecast_revision")
	record(t, w, 3, decidedAt, "no_skip", "")
	record(t, w, 4, decidedAt, "no_skip", "")
	record(t, w, 5, decidedAt, "no_skip", "")
	record(t, w, 6, decidedAt, "unknown", "no_fetch")
	record(t, w, 7, decidedAt, "unknown", "incomplete_fetch")
	record(t, w, 9, decidedAt, "no_skip", "")
	record(t, w, 10, decidedAt, "skip", "", "earnings")
	record(t, w, 11, "2026-10-01T01:00:00Z", "skip", "", "earnings")
	testkit.Outcome(t, w, 1, 10, 2, true, "tp", 40.0)
	testkit.Outcome(t, w, 2, 10, 2, false, "sl", -20.0)
	testkit.Outcome(t, w, 3, 10, 2, false, "sl", -50.0) // superseded by the next row (higher id)
	testkit.Outcome(t, w, 3, 5, 2, true, "timeout", 10.0)
	testkit.Outcome(t, w, 4, 10, 2, false, "sl", -30.0)
	testkit.Outcome(t, w, 6, 10, 2, true, "tp", 5.0)
	testkit.Outcome(t, w, 7, 10, 1, true, nil, nil)
	testkit.Outcome(t, w, 8, 10, 2, true, "tp", 1000.0)
	testkit.Outcome(t, w, 9, 10, 2, true, "tp", 777.0)
	testkit.Outcome(t, w, 10, 10, 2, true, "tp", 900.0)
	testkit.Outcome(t, w, 11, 10, 2, true, "tp", 900.0)
	return path, w
}
