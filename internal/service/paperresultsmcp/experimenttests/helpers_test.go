// Package experimenttests holds the HTTP-level tests of the tuning-experiment
// tools (issue #871): create / list / compare / close, the one-variable
// check, the confounder detection, no verdict before the minimum number of
// trades, and that compare's figures are those of paper_performance_summary
// and risk_rejections. The pure rules (validation, verdict) are tested in
// internal/service/paperresultsmcp/experiment; the narrow write path in
// settingstests.
package experimenttests

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
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/experiment"
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

// newServer serves the MCP over a migrated database seeded with SeedPositions
// and SeedRisk, with the fixed clock 2026-10-18 12:00 JST. w is a separate
// handle for planting rows and inspecting what was stored.
func newServer(t *testing.T) (*testServer, *sql.DB) { return newServerOn(t, "") }

// newServerOn is newServer with a registered database/sql driver ("" for the
// default), e.g. testkit.NewCountingDriver's, to see the statements run.
func newServerOn(t *testing.T, driverName string) (*testServer, *sql.DB) {
	t.Helper()
	path, w := testkit.NewMigratedDB(t)
	testkit.SeedPositions(t, w)
	testkit.SeedRisk(t, w)
	s := settings.Default()
	s.Enabled = true
	s.APIKey = testAPIKey
	srv := paperresultsmcp.New(paperresultsmcp.Options{
		Settings: s, DBPath: path, DriverName: driverName, Now: testkit.FixedNow, CapitalJPY: 30_000_000,
		Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)),
	})
	hs := httptest.NewServer(srv.Handler())
	t.Cleanup(func() {
		hs.Close()
		_ = srv.Close()
	})
	return &testServer{Server: srv, http: hs}, w
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

// data decodes a successful response's structuredContent.data into out.
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
		} `json:"result"`
	}
	if err := json.Unmarshal(r.body, &env); err != nil || env.Result.StructuredContent.Tool != tool {
		t.Fatalf("%s: bad envelope %s (%v)", tool, r.body, err)
	}
	if err := json.Unmarshal(env.Result.StructuredContent.Data, out); err != nil {
		t.Fatalf("%s: decode data %s: %v", tool, env.Result.StructuredContent.Data, err)
	}
}

// wantError asserts the HTTP status and the spec error code.
func (r response) wantError(t *testing.T, status int, code paperresultsmcp.Code) {
	t.Helper()
	var e struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(r.body, &e); err != nil || r.status != status || e.Code != string(code) {
		t.Fatalf("want %d %s, got %d %s (%s)", status, code, r.status, r.body, e.Code)
	}
}

func count(t *testing.T, w *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := w.QueryRowContext(t.Context(), query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// Times of the experiment most tests use, on JST day boundaries so that the
// same periods can be asked of paper_performance_summary / risk_rejections:
// the "after" period is the JST days 10-02 and 10-03, the "before" period the
// same length just before it (JST 09-30 and 10-01).
const (
	startedAt = "2026-10-01T15:00:00Z" // 2026-10-02 00:00 JST
	endedAt   = "2026-10-03T15:00:00Z" // 2026-10-04 00:00 JST
)

var (
	beforeDays = map[string]any{"from": "2026-09-30", "to": "2026-10-02"}
	afterDays  = map[string]any{"from": "2026-10-02", "to": "2026-10-04"}
)

func settingChange(name string, before, after any) map[string]any {
	return map[string]any{"kind": experiment.KindSetting, "name": name, "before": before, "after": after}
}

// createArgs is a valid experiment_create request changing `name` (a setting).
func createArgs(name string, extra map[string]any) map[string]any {
	args := map[string]any{
		"hypothesis": "1 回の最大損失を 1% から 0.5% に下げると、最大ドローダウンが減る",
		"changes":    []map[string]any{settingChange(name, 1, 0.5)},
		"started_at": startedAt,
	}
	for k, v := range extra {
		args[k] = v
	}
	return args
}

// create makes an experiment through the tool and returns it.
func create(t *testing.T, srv *testServer, args map[string]any) experiment.Experiment {
	t.Helper()
	var e experiment.Experiment
	srv.call(t, "experiment_create", args).data(t, "experiment_create", &e)
	return e
}

// closeAt ends experiment id at ended.
func closeAt(t *testing.T, srv *testServer, id int64, ended string) {
	t.Helper()
	srv.call(t, "experiment_close", map[string]any{"experiment_id": id, "ended_at": ended}).data(t, "experiment_close", &experiment.Experiment{})
}

func compare(t *testing.T, srv *testServer, id int64, extra map[string]any) experiment.Comparison {
	t.Helper()
	args := map[string]any{"experiment_id": id}
	for k, v := range extra {
		args[k] = v
	}
	var c experiment.Comparison
	srv.call(t, "experiment_compare", args).data(t, "experiment_compare", &c)
	return c
}
