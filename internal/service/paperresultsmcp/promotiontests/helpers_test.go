// Package promotiontests holds the HTTP-level tests of the promotion_checklist tool (issue #1091):
// proposal evidence over the candidate ledger's numbers, with criteria that can only be made
// stricter. It applies nothing. All data is synthetic (a migrated temp SQLite file).
package promotiontests

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/promotiondiag"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/settings"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

const (
	testAPIKey = "TEST-API-KEY-do-not-leak-0123456789"
	tool       = "promotion_checklist"
	eps        = 1e-9
)

func TestMain(m *testing.M) {
	code := m.Run()
	removeTemplate()
	testkit.RemoveTemplate()
	os.Exit(code)
}

type testServer struct {
	*paperresultsmcp.Server
	http *httptest.Server
}

func newServer(t *testing.T, driverName string) (*testServer, *sql.DB) {
	t.Helper()
	path, w := testkit.NewMigratedDB(t)
	s := settings.Default()
	s.Enabled = true
	s.APIKey = testAPIKey
	srv := paperresultsmcp.New(paperresultsmcp.Options{
		Settings: s, DBPath: path, DriverName: driverName, Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)),
		Now: testkit.FixedNow, CapitalJPY: 30_000_000, QueryTimeout: time.Minute,
	})
	hs := httptest.NewServer(srv.Handler())
	t.Cleanup(func() {
		hs.Close()
		_ = srv.Close()
	})
	return &testServer{Server: srv, http: hs}, w
}

type response struct {
	status int
	body   []byte
}

func (s *testServer) post(t *testing.T, name string, args any) response {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": name, "arguments": args}})
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

// period is the fixture's 12 JST days.
var period = map[string]any{"from": "2026-10-01", "to": "2026-10-13"}

func withPeriod(extra map[string]any) map[string]any {
	args := map[string]any{}
	for k, v := range period {
		args[k] = v
	}
	for k, v := range extra {
		args[k] = v
	}
	return args
}

// call runs the checklist for the candidate (a shadow version unless extra overrides it).
func (s *testServer) call(t *testing.T, version string, extra map[string]any) response {
	t.Helper()
	return s.post(t, tool, withPeriod(merge(map[string]any{"candidate": map[string]any{"kind": "shadow", "id": version}}, extra)))
}

func merge(a, b map[string]any) map[string]any {
	for k, v := range b {
		a[k] = v
	}
	return a
}

func (r response) data(t *testing.T) map[string]any {
	t.Helper()
	if r.status != http.StatusOK {
		t.Fatalf("status %d, body %s", r.status, r.body)
	}
	var env struct {
		Result struct {
			StructuredContent struct {
				Data map[string]any `json:"data"`
			} `json:"structuredContent"`
		} `json:"result"`
	}
	if err := json.Unmarshal(r.body, &env); err != nil {
		t.Fatalf("decode %s: %v", r.body, err)
	}
	return env.Result.StructuredContent.Data
}

// result decodes the data into the typed result.
func (r response) result(t *testing.T) promotiondiag.Result {
	t.Helper()
	b, _ := json.Marshal(r.data(t))
	var out promotiondiag.Result
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("decode result %s: %v", b, err)
	}
	return out
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

func item(t *testing.T, res promotiondiag.Result, name string) promotiondiag.Item {
	t.Helper()
	for _, it := range res.Items {
		if it.Name == name {
			return it
		}
	}
	t.Fatalf("no item %s", name)
	return promotiondiag.Item{}
}

// dump hashes every row of every table, so any write by the tool changes the result.
func dump(t *testing.T, w *sql.DB) map[string]string {
	t.Helper()
	rows, err := w.QueryContext(t.Context(), `SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		names = append(names, n)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	_ = rows.Close()
	out := map[string]string{}
	for _, n := range names {
		r, err := w.QueryContext(t.Context(), `SELECT * FROM "`+n+`"`)
		if err != nil {
			t.Fatal(err)
		}
		cols, _ := r.Columns()
		h := sha256.New()
		for r.Next() {
			vals, ptrs := make([]any, len(cols)), make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			if err := r.Scan(ptrs...); err != nil {
				t.Fatal(err)
			}
			_, _ = fmt.Fprintf(h, "%v\n", vals)
		}
		if err := r.Err(); err != nil {
			t.Fatal(err)
		}
		_ = r.Close()
		out[n] = fmt.Sprintf("%x", h.Sum(nil))
	}
	return out
}

func testkitExec(t *testing.T, w *sql.DB, q string, args ...any) {
	t.Helper()
	testkit.MustExec(t, w, q, args...)
}

// testkitNullCost forgets the recorded request_cost of one live decision.
func testkitNullCost(t *testing.T, w *sql.DB) {
	t.Helper()
	testkitExec(t, w, `UPDATE jev_decisions SET request_cost = NULL WHERE id = 1`)
}

// postRaw sends a JSON-RPC method without parameters.
func (s *testServer) postRaw(t *testing.T, method string) response {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method})
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
