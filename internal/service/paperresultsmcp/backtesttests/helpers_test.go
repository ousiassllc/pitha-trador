// Package backtesttests holds the HTTP-level tests of the research-backtest
// tools (issue #1086): backtest_start / backtest_get / backtest_list over a
// migrated temp SQLite and a fake replay input that the real replay engine
// runs (labtest). No network, no sleeping: runs are waited for through the
// job manager's finish hook, and the blocking and timeout cases use channels
// and an injected short cap. The lab's own rules are tested in
// internal/service/backtestlab, the record in internal/repository/backtestrun.
package backtesttests

import (
	"bytes"
	"context"
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

	"github.com/ousiassllc/pitha-trador/internal/service/backtestlab"
	"github.com/ousiassllc/pitha-trador/internal/service/backtestlab/labtest"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/backtesttool"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/settings"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

const testAPIKey = "TEST-API-KEY-do-not-leak-0123456789"

func TestMain(m *testing.M) {
	code := m.Run()
	testkit.RemoveTemplate()
	os.Exit(code)
}

// The period of labtest.Configs as the tools take it.
var labPeriod = map[string]any{"from": "2026-10-05", "to": "2026-10-07"}

type testServer struct {
	*paperresultsmcp.Server
	http     *httptest.Server
	w        *sql.DB // the handle for planting rows and inspecting what was stored
	path     string
	finished chan int64
	logs     *bytes.Buffer
	modes    chan string // the replay modes the factory was asked for
}

// options tweaks what newServer passes to paperresultsmcp.New.
type options struct {
	source  func(ctx context.Context, mode string) (backtestlab.ConfigSource, error)
	timeout time.Duration
	owner   string
	driver  string
}

func newServer(t *testing.T, o options) *testServer {
	t.Helper()
	path, w := testkit.NewMigratedDB(t)
	testkit.SeedPositions(t, w)
	testkit.SeedRisk(t, w)
	s := settings.Default()
	s.Enabled = true
	s.APIKey = testAPIKey
	ts := &testServer{w: w, path: path, finished: make(chan int64, 64), logs: &bytes.Buffer{}, modes: make(chan string, 64)}
	if o.source == nil {
		o.source = func(context.Context, string) (backtestlab.ConfigSource, error) { return &labtest.Source{}, nil }
	}
	inner := o.source
	opts := paperresultsmcp.Options{
		Settings: s, DBPath: path, DriverName: o.driver, Now: testkit.FixedNow, CapitalJPY: 30_000_000,
		Logger: slog.New(slog.NewJSONHandler(ts.logs, nil)),
		Backtest: backtesttool.Options{
			Source: func(ctx context.Context, mode string) (backtestlab.ConfigSource, error) {
				ts.modes <- mode
				return inner(ctx, mode)
			},
			OnFinish:   func(id int64) { ts.finished <- id },
			RunTimeout: o.timeout, Owner: o.owner,
		},
	}
	ts.Server = paperresultsmcp.New(opts)
	ts.http = httptest.NewServer(ts.Handler())
	t.Cleanup(func() {
		ts.http.Close()
		_ = ts.Close()
	})
	return ts
}

// noSource is newServer's option for a server without a replay input.
func newServerWithoutSource(t *testing.T) *testServer {
	t.Helper()
	path, w := testkit.NewMigratedDB(t)
	s := settings.Default()
	s.Enabled = true
	s.APIKey = testAPIKey
	ts := &testServer{w: w, path: path}
	ts.Server = paperresultsmcp.New(paperresultsmcp.Options{Settings: s, DBPath: path, Now: testkit.FixedNow, Logger: slog.New(slog.NewJSONHandler(io.Discard, nil))})
	ts.http = httptest.NewServer(ts.Handler())
	t.Cleanup(func() {
		ts.http.Close()
		_ = ts.Close()
	})
	return ts
}

type response struct {
	status int
	body   []byte
}

func (s *testServer) rpc(t *testing.T, method string, params any) response {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, s.http.URL, strings.NewReader(string(body)))
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
	return s.rpc(t, "tools/call", map[string]any{"name": name, "arguments": args})
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

type failure struct {
	Code  string `json:"code"`
	Error struct {
		Data struct {
			Detail string `json:"detail"`
		} `json:"data"`
	} `json:"error"`
}

// wantError asserts the HTTP status and the spec error code and returns the detail.
func (r response) wantError(t *testing.T, status int, code paperresultsmcp.Code) string {
	t.Helper()
	var e failure
	if err := json.Unmarshal(r.body, &e); err != nil || r.status != status || e.Code != string(code) {
		t.Fatalf("want %d %s, got %d %s (%s)", status, code, r.status, r.body, e.Code)
	}
	return e.Error.Data.Detail
}

// start calls backtest_start and returns the queued run.
func (s *testServer) start(t *testing.T, args map[string]any) runView {
	t.Helper()
	var v runView
	s.call(t, "backtest_start", withPeriod(args)).data(t, "backtest_start", &v)
	return v
}

// withPeriod adds labPeriod to args unless they name their own.
func withPeriod(args map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range labPeriod {
		out[k] = v
	}
	for k, v := range args {
		out[k] = v
	}
	return out
}

// waitFinished waits (bounded) for the end of run id.
func (s *testServer) waitFinished(t *testing.T, id int64) {
	t.Helper()
	deadline := time.After(30 * time.Second)
	for {
		select {
		case got := <-s.finished:
			if got == id {
				return
			}
		case <-deadline:
			t.Fatalf("run %d did not finish", id)
		}
	}
}

func (s *testServer) get(t *testing.T, id int64) detailView {
	t.Helper()
	var d detailView
	s.call(t, "backtest_get", map[string]any{"run_id": id}).data(t, "backtest_get", &d)
	return d
}

// startAndWait runs one backtest to its end and returns it.
func (s *testServer) startAndWait(t *testing.T, args map[string]any) detailView {
	t.Helper()
	r := s.start(t, args)
	s.waitFinished(t, r.RunID)
	return s.get(t, r.RunID)
}
