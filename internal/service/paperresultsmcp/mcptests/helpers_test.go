package mcptests

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/settings"
)

const testAPIKey = "TEST-API-KEY-do-not-leak-0123456789"

// lockedBuffer is a goroutine-safe log sink.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func newJSONLogger(w io.Writer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// testServer is a Server over httptest plus its captured log.
type testServer struct {
	*paperresultsmcp.Server
	http *httptest.Server
	logs *lockedBuffer
}

func enabledSettings() settings.Settings {
	s := settings.Default()
	s.Enabled = true
	s.APIKey = testAPIKey
	return s
}

// newTestServer serves Handler on httptest (no TCP listen guard involved).
func newTestServer(t *testing.T, opts paperresultsmcp.Options) *testServer {
	t.Helper()
	logs := &lockedBuffer{}
	opts.Logger = newJSONLogger(logs)
	if opts.Now == nil {
		opts.Now = testkit.FixedNow
	}
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

// response is one HTTP exchange.
type response struct {
	status int
	body   []byte
}

// post sends body with the given Authorization header ("" omits it).
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
	return s.do(t, req)
}

func (s *testServer) do(t *testing.T, req *http.Request) response {
	t.Helper()
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

func rpc(method string, params any) string {
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	return string(b)
}

func callBody(name string, args any) string {
	return rpc("tools/call", map[string]any{"name": name, "arguments": args})
}

// call invokes a tool with the right key and returns the HTTP response.
func (s *testServer) call(t *testing.T, name string, args any) response {
	t.Helper()
	return s.post(t, "Bearer "+testAPIKey, callBody(name, args))
}

// data decodes a successful tool response's structuredContent.data into out
// and checks the envelope.
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
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
		} `json:"result"`
	}
	if err := json.Unmarshal(r.body, &env); err != nil {
		t.Fatalf("%s: decode %s: %v", tool, r.body, err)
	}
	sc := env.Result.StructuredContent
	if sc.Tool != tool || env.Result.IsError {
		t.Fatalf("%s: bad envelope %s", tool, r.body)
	}
	if len(env.Result.Content) != 1 {
		t.Fatalf("%s: want one text content, got %s", tool, r.body)
	}
	if err := json.Unmarshal(sc.Data, out); err != nil {
		t.Fatalf("%s: decode data %s: %v", tool, sc.Data, err)
	}
}

// fixedMessages are the client-visible messages of
// docs/architecture/paper-results-mcp-errors.md §1, restated here so the
// tests do not depend on the implementation's own table.
var fixedMessages = map[paperresultsmcp.Code]string{
	paperresultsmcp.CodeUnauthorized:       "認証に失敗しました。",
	paperresultsmcp.CodeAccessDenied:       "認証に失敗しました。",
	paperresultsmcp.CodeInvalidPeriod:      "期間の指定が不正です。",
	paperresultsmcp.CodeInvalidArgument:    "引数が不正です。",
	paperresultsmcp.CodeLimitExceeded:      "取得量が上限を超えています。期間を狭めてください。",
	paperresultsmcp.CodeDBLocked:           "データベースが使用中です。しばらくしてから再度呼んでください。",
	paperresultsmcp.CodeDBUnavailable:      "データベースを読み取れません。",
	paperresultsmcp.CodeQueryTimeout:       "読み取りが時間内に終わりませんでした。",
	paperresultsmcp.CodeConcurrencyLimited: "同時に処理できる件数を超えています。",
	paperresultsmcp.CodeInternal:           "読み取りに失敗しました。",
}

// errorBody is the spec error envelope.
type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (r response) errorBody(t *testing.T) errorBody {
	t.Helper()
	var e errorBody
	if err := json.Unmarshal(r.body, &e); err != nil {
		t.Fatalf("decode error body %s: %v", r.body, err)
	}
	return e
}

// wantError asserts the HTTP status, code and fixed message of the errors
// document.
func (r response) wantError(t *testing.T, status int, code paperresultsmcp.Code) {
	t.Helper()
	e := r.errorBody(t)
	if r.status != status || e.Code != string(code) || e.Message != fixedMessages[code] {
		t.Fatalf("want %d %s %q, got %d %s %q (%s)", status, code, fixedMessages[code], r.status, e.Code, e.Message, r.body)
	}
}

// seededServer is a server over a fully seeded database.
func seededServer(t *testing.T, mutate func(*paperresultsmcp.Options)) *testServer {
	t.Helper()
	path, w := testkit.NewMigratedDB(t)
	testkit.SeedAll(t, w)
	opts := paperresultsmcp.Options{Settings: enabledSettings(), DBPath: path}
	if mutate != nil {
		mutate(&opts)
	}
	return newTestServer(t, opts)
}

// period is the explicit period most tool tests use: JST 2026-10-01..10-10.
var period = map[string]any{"from": "2026-10-01", "to": "2026-10-10"}

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
