// Package virtualpapertests holds the HTTP-level tests of the virtual Paper
// tools (issue #1087): create / clone / stop behind the write switch, the caps,
// the comparison over the common days, and that nothing but the profile rows is
// written and no secret is shown. They live apart from mcptests, which holds the
// read tools' tests.
package virtualpapertests

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
	logs *strings.Builder
}

type response struct {
	status int
	body   []byte
}

// newServer serves Handler on httptest (the fixed clock, 2026-10-18 12:00 JST)
// over a migrated database, plus a separate handle to seed and inspect it.
func newServer(t *testing.T) (*testServer, *sql.DB) {
	t.Helper()
	path, w := testkit.NewMigratedDB(t)
	s := settings.Default()
	s.Enabled = true
	s.APIKey = testAPIKey
	logs := &strings.Builder{}
	srv := paperresultsmcp.New(paperresultsmcp.Options{
		Settings: s, DBPath: path, CapitalJPY: 30_000_000, Now: testkit.FixedNow,
		Logger: slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
	})
	hs := httptest.NewServer(srv.Handler())
	t.Cleanup(func() {
		hs.Close()
		_ = srv.Close()
	})
	return &testServer{Server: srv, http: hs, logs: logs}, w
}

func (s *testServer) post(t *testing.T, body string) response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, s.http.URL, strings.NewReader(body))
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
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": name, "arguments": args}})
	return s.post(t, string(b))
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

// wantError asserts the HTTP status and code, and returns error.data.detail.
func (r response) wantError(t *testing.T, status int, code paperresultsmcp.Code) string {
	t.Helper()
	var e struct {
		Code  string `json:"code"`
		Error struct {
			Data map[string]string `json:"data"`
		} `json:"error"`
	}
	if err := json.Unmarshal(r.body, &e); err != nil {
		t.Fatalf("decode error body %s: %v", r.body, err)
	}
	if r.status != status || e.Code != string(code) {
		t.Fatalf("want %d %s, got %d %s (%s)", status, code, r.status, e.Code, r.body)
	}
	return e.Error.Data["detail"]
}

func count(t *testing.T, w *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := w.QueryRowContext(t.Context(), query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// at is a JST wall-clock time.
func at(day string, h, m int) time.Time {
	d, err := time.ParseInLocation(time.DateOnly, day, time.FixedZone("JST", 9*3600))
	if err != nil {
		panic(err)
	}
	return d.Add(time.Duration(h)*time.Hour + time.Duration(m)*time.Minute)
}

type profileView struct {
	ID         int64          `json:"profile_id"`
	Name       string         `json:"name"`
	Status     string         `json:"status"`
	Params     map[string]any `json:"params"`
	FirstDay   string         `json:"first_day"`
	ClonedFrom *int64         `json:"cloned_from"`
}

func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }
