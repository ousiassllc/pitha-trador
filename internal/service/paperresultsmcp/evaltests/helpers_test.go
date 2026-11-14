// Package evaltests holds the HTTP-level tests of the jev_evaluation tool (issue
// #1073): every trader LONG/SHORT decision against same-row baselines with the
// Labeler's own label. They live apart from mcptests, which holds the older read
// tools' tests. All data is synthetic (a migrated temp SQLite file).
package evaltests

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
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/evaldiag"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/settings"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

const (
	testAPIKey = "TEST-API-KEY-do-not-leak-0123456789"
	tool       = "jev_evaluation"
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
		Now: testkit.FixedNow, CapitalJPY: 30_000_000,
		// The limit tests aggregate 20,000 decisions; under -race on a loaded CI runner that exceeds the
		// production 3 s budget, which is not what these tests pin (the limits and the numbers are).
		QueryTimeout: time.Minute,
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

// data decodes the successful tool response's structuredContent.data.
func (r response) data(t *testing.T) evaldiag.Data {
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
	var d evaldiag.Data
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

// cohortOf finds the (model, version) cohort or fails.
func cohortOf(t *testing.T, d evaldiag.Data, model, version string) evaldiag.Cohort {
	t.Helper()
	for _, c := range d.Cohorts {
		if c.ModelID == model && c.QuestionVersion == version {
			return c
		}
	}
	t.Fatalf("no cohort %s/%s in %+v", model, version, d.Cohorts)
	return evaldiag.Cohort{}
}

func armOf(t *testing.T, c evaldiag.Cohort, name string) evaldiag.Arm {
	t.Helper()
	for _, a := range c.Arms {
		if a.Arm == name {
			return a
		}
	}
	t.Fatalf("no arm %s", name)
	return evaldiag.Arm{}
}

func diffOf(t *testing.T, c evaldiag.Cohort, baseline string) evaldiag.Difference {
	t.Helper()
	for _, d := range c.Differences {
		if d.Baseline == baseline {
			return d
		}
	}
	t.Fatalf("no difference against %s", baseline)
	return evaldiag.Difference{}
}

func reasonCount(c evaldiag.Cohort, reason string) int {
	for _, b := range c.ExcludedByReason {
		if b.Reason == reason {
			return b.Count
		}
	}
	return -1
}

// wantMean fails unless m has n rows and the given mean (within eps).
func wantMean(t *testing.T, what string, m evaldiag.Metrics, n int, mean float64) {
	t.Helper()
	if m.N != n || m.MeanNetBps == nil || math.Abs(*m.MeanNetBps-mean) > eps {
		t.Errorf("%s: n=%d mean=%v, want n=%d mean=%v", what, m.N, deref(m.MeanNetBps), n, mean)
	}
}

func deref(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}
