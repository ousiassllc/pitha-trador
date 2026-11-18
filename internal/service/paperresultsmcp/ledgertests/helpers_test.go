// Package ledgertests holds the HTTP-level tests of the candidate_ledger tool (issue #1090):
// the live Jev and its candidates on the same period, the same decision rows and the same
// cost-inclusive label. All data is synthetic (a migrated temp SQLite file).
package ledgertests

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/ledgerdiag"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/settings"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

const (
	testAPIKey = "TEST-API-KEY-do-not-leak-0123456789"
	tool       = "candidate_ledger"
	eps        = 1e-9
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

// newServer serves the MCP over a migrated database; driverName ("" for the default) is a
// registered database/sql driver, e.g. testkit.NewCountingDriver's.
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

func (s *testServer) post(t *testing.T, method string, params any) response {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
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

func (s *testServer) call(t *testing.T, extra map[string]any) response {
	t.Helper()
	args := map[string]any{"from": "2026-10-05", "to": "2026-10-09"}
	for k, v := range extra {
		args[k] = v
	}
	return s.post(t, "tools/call", map[string]any{"name": tool, "arguments": args})
}

// raw returns the tool's data as generic JSON.
func (r response) raw(t *testing.T) map[string]any {
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

// ledger decodes the successful response's data into the typed ledger (the fields it exposes).
func (r response) ledger(t *testing.T) ledgerdiag.Ledger {
	t.Helper()
	data, err := json.Marshal(r.raw(t))
	if err != nil {
		t.Fatal(err)
	}
	var l ledgerdiag.Ledger
	if err := json.Unmarshal(data, &l); err != nil {
		t.Fatalf("decode ledger %s: %v", data, err)
	}
	return l
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

func entryOf(t *testing.T, l ledgerdiag.Ledger, id string) ledgerdiag.Entry {
	t.Helper()
	for _, e := range l.Candidates {
		if e.ID == id {
			return e
		}
	}
	t.Fatalf("no candidate %s in %d candidates", id, len(l.Candidates))
	return ledgerdiag.Entry{}
}

func near(a *float64, want float64) bool { return a != nil && math.Abs(*a-want) <= eps }

func deref(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}
