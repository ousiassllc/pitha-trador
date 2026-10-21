// Package proposaltests holds the HTTP-level tests of the order-proposal tools
// (issue #827): the intake switch, the transaction that records a proposal and
// its first event, idempotency, the argument rules, the daily limit and the
// narrow write path. They live apart from settingstests, which holds the
// settings tools' tests.
package proposaltests

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

// newTestServer serves Handler on httptest with the fixed clock
// (2026-10-18 12:00 JST).
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

// proposalServer is a server over a migrated database plus a separate writer
// handle to inspect (and seed) what was stored. The intake switch is off, as
// it is for a fresh installation.
func proposalServer(t *testing.T) (*testServer, *sql.DB) {
	t.Helper()
	path, w := testkit.NewMigratedDB(t)
	return newTestServer(t, paperresultsmcp.Options{Settings: enabledSettings(), DBPath: path}), w
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
	paperresultsmcp.CodeUnauthorized:        "認証に失敗しました。",
	paperresultsmcp.CodeInvalidArgument:     "引数が不正です。",
	paperresultsmcp.CodeLimitExceeded:       "取得量が上限を超えています。期間を狭めてください。",
	paperresultsmcp.CodeKeyNotAllowed:       "このキーは MCP から変更できません。settings_get に出るキーだけ変更できます。",
	paperresultsmcp.CodeProposalsDisabled:   "注文案の受け付けはオフです。Settings の「外部連携」でオンにしてください（MCP からはオンにできません）。",
	paperresultsmcp.CodeIdempotencyConflict: "この idempotency_key は内容の違う案で使われています。別のキーで出し直してください。",
	paperresultsmcp.CodeProposalNotFound:    "その proposal_id の注文案はありません。",
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

// wantError asserts the HTTP status, code and fixed message and returns the
// client-visible detail ("" when there is none).
func (r response) wantError(t *testing.T, status int, code paperresultsmcp.Code) string {
	t.Helper()
	e := r.errorBody(t)
	if r.status != status || e.Code != string(code) || e.Message != fixedMessages[code] {
		t.Fatalf("want %d %s %q, got %d %s %q (%s)", status, code, fixedMessages[code], r.status, e.Code, e.Message, r.body)
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

// writtenTables are every table a proposal call could conceivably change.
var writtenTables = []string{
	"order_proposals", "order_proposal_events", "runtime_settings", "mcp_settings_audit",
	"paper_orders", "positions", "trade_signals", "kill_switch_events",
}

// rowCounts is the row count of each of writtenTables.
func rowCounts(t *testing.T, w *sql.DB) map[string]int {
	t.Helper()
	out := map[string]int{}
	for _, table := range writtenTables {
		out[table] = count(t, w, `SELECT COUNT(*) FROM `+table)
	}
	return out
}

func wantSameCounts(t *testing.T, w *sql.DB, before map[string]int) {
	t.Helper()
	after := rowCounts(t, w)
	for table, n := range before {
		if after[table] != n {
			t.Errorf("%s: %d rows, was %d", table, after[table], n)
		}
	}
}

// switchOn stores the intake switch the way the Settings screen does.
func switchOn(t *testing.T, w *sql.DB) {
	t.Helper()
	testkit.MustExec(t, w, `INSERT INTO runtime_settings (key, value, updated_at) VALUES ('mcp.order_proposals_enabled', '1', 't')
		ON CONFLICT (key) DO UPDATE SET value = '1'`)
}

// validProposal is a correct open_long proposal; each test changes what it
// tests. expires_at is 23 hours after the fixed clock.
func validProposal(key string) map[string]any {
	return map[string]any{
		"idempotency_key": key,
		"symbol":          "7203",
		"bucket":          "stock",
		"action":          "open_long",
		"quantity":        100,
		"order_type":      "LIMIT",
		"limit_price":     2500,
		"stop_loss_price": 2450,
		"exit_plan":       "決算発表前の引けで手じまう",
		"max_loss_jpy":    5000,
		"rationale":       "直近の決算が好調で、需給も良い",
		"sources": []map[string]any{
			{"title": "決算短信", "url": "https://example.com/ir/2026q2.pdf", "retrieved_at": "2026-10-18T09:30:00+09:00"},
		},
		"expires_at": "2026-10-19T02:00:00Z",
	}
}

func without(args map[string]any, keys ...string) map[string]any {
	out := map[string]any{}
	for k, v := range args {
		out[k] = v
	}
	for _, k := range keys {
		delete(out, k)
	}
	return out
}

func with(args map[string]any, kv ...any) map[string]any {
	out := without(args)
	for i := 0; i < len(kv); i += 2 {
		out[kv[i].(string)] = kv[i+1]
	}
	return out
}

// submitted is order_proposal_submit's data.
type submitted struct {
	ProposalID int64  `json:"proposal_id"`
	Status     string `json:"status"`
	ReceivedAt string `json:"received_at"`
	ExpiresAt  string `json:"expires_at"`
	Replayed   bool   `json:"replayed"`
}

func submit(t *testing.T, srv *testServer, args map[string]any) submitted {
	t.Helper()
	var out submitted
	srv.call(t, "order_proposal_submit", args).data(t, "order_proposal_submit", &out)
	return out
}
