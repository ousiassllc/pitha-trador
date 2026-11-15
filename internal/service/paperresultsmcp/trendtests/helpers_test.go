// Package trendtests holds the HTTP-level tests of the jev_evaluation_trend tool (issue
// #1089): the daily record of the Jev evaluation read back as series. They live apart from
// mcptests, which holds the older read tools' tests. All data is synthetic (a migrated temp
// SQLite file); the record is made by the production recorder, never by the code under test.
package trendtests

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
	"testing"
	"time"

	repo "github.com/ousiassllc/pitha-trador/internal/repository/evalrecord"
	"github.com/ousiassllc/pitha-trador/internal/service/evalrecord"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/settings"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/trenddiag"
)

const (
	testAPIKey = "TEST-API-KEY-do-not-leak-0123456789"
	tool       = "jev_evaluation_trend"
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

func newServer(t *testing.T) (*testServer, *sql.DB) {
	t.Helper()
	path, w := testkit.NewMigratedDB(t)
	s := settings.Default()
	s.Enabled = true
	s.APIKey = testAPIKey
	srv := paperresultsmcp.New(paperresultsmcp.Options{
		Settings: s, DBPath: path, Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)),
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

// call is a call over 2026-10-05..2026-10-09 (JST, the end exclusive) unless extra says otherwise.
func (s *testServer) call(t *testing.T, extra map[string]any) response {
	t.Helper()
	args := map[string]any{"from": "2026-10-05", "to": "2026-10-10"}
	for k, v := range extra {
		args[k] = v
	}
	return s.post(t, "tools/call", map[string]any{"name": tool, "arguments": args})
}

// data decodes the successful tool response's structuredContent.data.
func (r response) data(t *testing.T) trenddiag.Data {
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
	var d trenddiag.Data
	if err := json.Unmarshal(env.Result.StructuredContent.Data, &d); err != nil {
		t.Fatalf("decode data %s: %v", env.Result.StructuredContent.Data, err)
	}
	return d
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

var jst = time.FixedZone("JST", 9*60*60)

// at is 09:00 JST of 2026-10-<day> (+ minutes).
func at(day, minutes int) time.Time { return time.Date(2026, 10, day, 9, minutes, 0, 0, jst) }

func f(v float64) *float64 { return &v }

// recorder is the production recorder over w with its clock at 16:30 JST of 2026-10-<day>.
func recorder(w *sql.DB, day int) *evalrecord.Recorder {
	now := time.Date(2026, 10, day, 16, 30, 0, 0, jst)
	return evalrecord.New(repo.NewRepository(w), w, func(context.Context) (int, error) { return 365, nil }).WithClock(func() time.Time { return now })
}

// recordPending runs the recorder until nothing is pending (n bounds the runs).
func recordPending(t *testing.T, r *evalrecord.Recorder) {
	t.Helper()
	for range 20 {
		res, err := r.Run(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if !res.More {
			return
		}
	}
	t.Fatal("the recorder did not finish")
}

// seriesOf finds the (model, version, horizon) series or fails.
func seriesOf(t *testing.T, d trenddiag.Data, model, version string) trenddiag.Series {
	t.Helper()
	for _, s := range d.Series {
		if s.ModelID == model && s.QuestionVersion == version {
			return s
		}
	}
	t.Fatalf("no series for %s/%s in %+v", model, version, d.Series)
	return trenddiag.Series{}
}
