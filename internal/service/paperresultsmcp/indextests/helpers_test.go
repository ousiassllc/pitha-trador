// Package fxtests holds the HTTP-level tests of the index_series_summary tool and
// the at-or-before join (issue #1084). They live apart from mcptests, which holds
// the older read tools' tests. All data is synthetic (a migrated temp SQLite file).
package indextests

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

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/indexdiag"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/settings"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

const (
	testAPIKey = "TEST-API-KEY-do-not-leak-0123456789"
	toolName   = "index_series_summary"
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

func newServer(t *testing.T) (*testServer, *sql.DB) {
	t.Helper()
	path, w := testkit.NewMigratedDB(t)
	s := settings.Default()
	s.Enabled = true
	s.APIKey = testAPIKey
	srv := paperresultsmcp.New(paperresultsmcp.Options{
		Settings: s, DBPath: path, Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)),
		Now: testkit.FixedNow, CapitalJPY: 30_000_000,
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

func (s *testServer) call(t *testing.T, args map[string]any) response {
	t.Helper()
	return s.post(t, "tools/call", map[string]any{"name": toolName, "arguments": args})
}

// data decodes the successful tool response's structuredContent.data.
func (r response) data(t *testing.T) indexdiag.Data {
	t.Helper()
	if r.status != http.StatusOK {
		t.Fatalf("status %d, body %s", r.status, r.body)
	}
	var env struct {
		Result struct {
			StructuredContent struct {
				Data indexdiag.Data `json:"data"`
			} `json:"structuredContent"`
		} `json:"result"`
	}
	if err := json.Unmarshal(r.body, &env); err != nil {
		t.Fatalf("decode %s: %v", r.body, err)
	}
	return env.Result.StructuredContent.Data
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

// jst is the zone of the trading day.
var jst = time.FixedZone("JST", 9*3600)

// at is the JST time hm of date, in UTC.
func at(date, hm string) time.Time {
	t, err := time.ParseInLocation("2006-01-02 15:04", date+" "+hm, jst)
	if err != nil {
		panic(err)
	}
	return t.UTC()
}

// value inserts the recorded value of symbol for the session minute hm (JST) of date.
func value(t *testing.T, w *sql.DB, symbol, date, hm string, price float64) {
	t.Helper()
	testkit.MustExec(t, w, `INSERT INTO research_index_minutes (symbol, minute_time, trading_day, price, via, recorded_at) VALUES (?, ?, ?, ?, 'event', 't')`,
		symbol, sqlutil.FormatTime(at(date, hm)), date, price)
}

func mark(t *testing.T, w *sql.DB, symbol, date, status string, n int) {
	t.Helper()
	testkit.MustExec(t, w, `INSERT INTO research_index_days (symbol, trading_day, status, minute_count, checked_at) VALUES (?, ?, ?, ?, 't')`, symbol, date, status, n)
}

// source inserts a row of the 立花 quote recording (research_quote_samples).
func source(t *testing.T, w *sql.DB, symbol, date, hm string) {
	t.Helper()
	testkit.MustExec(t, w, `INSERT INTO research_quote_samples (timestamp, source, symbol, price, via, session_phase) VALUES (?, 'tachibana', ?, 1, 'event', 'morning')`,
		sqlutil.FormatTime(at(date, hm)), symbol)
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
			vals := make([]any, len(cols))
			ptrs := make([]any, len(cols))
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
