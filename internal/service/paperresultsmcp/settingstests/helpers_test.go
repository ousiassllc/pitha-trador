// Package settingstests holds the HTTP-level tests of the settings tools
// (issues #806, #807): list -> change -> diff, the whitelist, the write
// switch, the audit log and the narrow write path. They live apart from
// mcptests, which holds the read tools' tests.
package settingstests

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
	logs *strings.Builder
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

// newTestServer serves Handler on httptest with the fixed clock.
func newTestServer(t *testing.T, opts paperresultsmcp.Options) *testServer {
	t.Helper()
	logs := &strings.Builder{}
	opts.Logger = slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
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
	return &testServer{Server: srv, http: hs, logs: logs}
}

// settingsServer is a server over a migrated database plus a separate writer
// handle to inspect what was stored.
func settingsServer(t *testing.T, mutate func(*paperresultsmcp.Options)) (*testServer, *sql.DB) {
	t.Helper()
	path, w := testkit.NewMigratedDB(t)
	opts := paperresultsmcp.Options{Settings: enabledSettings(), DBPath: path}
	if mutate != nil {
		mutate(&opts)
	}
	return newTestServer(t, opts), w
}

func (s *testServer) post(t *testing.T, auth, body string) response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, s.http.URL, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
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

func callBody(name string, args any) string {
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": name, "arguments": args}})
	return string(b)
}

func (s *testServer) call(t *testing.T, name string, args any) response {
	t.Helper()
	return s.post(t, "Bearer "+testAPIKey, callBody(name, args))
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

// fixedMessages restate docs/architecture/paper-results-mcp-errors.md §1 for
// the codes these tests expect.
var fixedMessages = map[paperresultsmcp.Code]string{
	paperresultsmcp.CodeUnauthorized:         "認証に失敗しました。",
	paperresultsmcp.CodeInvalidArgument:      "引数が不正です。",
	paperresultsmcp.CodeLimitExceeded:        "取得量が上限を超えています。期間を狭めてください。",
	paperresultsmcp.CodeWriteDisabled:        "設定の書き込みは許可されていません。settings_write_enable か Settings の「外部連携」で許可してください。",
	paperresultsmcp.CodeKeyNotAllowed:        "このキーは MCP から変更できません。settings_get に出るキーだけ変更できます。",
	paperresultsmcp.CodeInvalidValue:         "値が不正です。settings_get の constraint を確認してください。",
	paperresultsmcp.CodeMultipleVariables:    "1 つの実験で変えられる変数は 1 つだけです。changes を 1 つにして、変数ごとに実験を分けてください。",
	paperresultsmcp.CodeExperimentNotRunning: "この実験はもう終わっています（再び開くことはできません）。",
}

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Error   struct {
		Data map[string]string `json:"data"`
	} `json:"error"`
}

func (r response) errorBody(t *testing.T) errorBody {
	t.Helper()
	var e errorBody
	if err := json.Unmarshal(r.body, &e); err != nil {
		t.Fatalf("decode error body %s: %v", r.body, err)
	}
	return e
}

// wantError asserts the HTTP status, code and fixed message.
func (r response) wantError(t *testing.T, status int, code paperresultsmcp.Code) {
	t.Helper()
	e := r.errorBody(t)
	if r.status != status || e.Code != string(code) || e.Message != fixedMessages[code] {
		t.Fatalf("want %d %s %q, got %d %s %q (%s)", status, code, fixedMessages[code], r.status, e.Code, e.Message, r.body)
	}
}

func set(srv *testServer, t *testing.T, key string, value any) response {
	t.Helper()
	return srv.call(t, "settings_set", map[string]any{"key": key, "value": value})
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

// settingsTools are the settings tools of the dispatch table.
var settingsTools = []string{
	"settings_get", "settings_diff", "settings_audit", "settings_set", "settings_write_enable", "settings_write_disable",
}
