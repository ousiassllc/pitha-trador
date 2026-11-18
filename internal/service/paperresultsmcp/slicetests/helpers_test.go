// Package slicetests holds the HTTP-level tests of the jev_evaluation_slices tool
// (issue #1089): the jev_evaluation LLM arm cut by features, with a Holm
// correction inside each family of cuts. All data is synthetic (a migrated temp
// SQLite file); every expected number is computed in the test, not by the tool.
package slicetests

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/settings"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/slicediag"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

const (
	testAPIKey = "TEST-API-KEY-do-not-leak-0123456789"
	tool       = "jev_evaluation_slices"
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

// newServer starts the real MCP server on a migrated temp DB; driver ("" for the default)
// names the database/sql driver it reads through.
func newServer(t *testing.T) (*testServer, *sql.DB) {
	t.Helper()
	path, w := testkit.NewMigratedDB(t)
	return startServer(t, path, ""), w
}

func startServer(t *testing.T, path, driver string) *testServer {
	t.Helper()
	s := settings.Default()
	s.Enabled = true
	s.APIKey = testAPIKey
	srv := paperresultsmcp.New(paperresultsmcp.Options{
		Settings: s, DBPath: path, DriverName: driver, Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)),
		Now: testkit.FixedNow, CapitalJPY: 30_000_000,
		// The limit tests read MaxDecisions decisions; under -race on a loaded CI runner that exceeds the
		// production 3 s budget, which is not what these tests pin (the limits and the numbers are).
		QueryTimeout: time.Minute,
	})
	hs := httptest.NewServer(srv.Handler())
	t.Cleanup(func() {
		hs.Close()
		_ = srv.Close()
	})
	return &testServer{Server: srv, http: hs}
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

// call runs the tool over 2026-10-05 .. 2026-10-11 JST (to is exclusive) unless extra overrides it.
func (s *testServer) call(t *testing.T, extra map[string]any) response {
	t.Helper()
	args := map[string]any{"from": "2026-10-05", "to": "2026-10-12"}
	for k, v := range extra {
		args[k] = v
	}
	return s.post(t, "tools/call", map[string]any{"name": tool, "arguments": args})
}

// data decodes the successful tool response's structuredContent.data.
func (r response) data(t *testing.T) slicediag.Data {
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
	var d slicediag.Data
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
func cohortOf(t *testing.T, d slicediag.Data, model, version string) slicediag.Cohort {
	t.Helper()
	for _, c := range d.Cohorts {
		if c.ModelID == model && c.QuestionVersion == version {
			return c
		}
	}
	t.Fatalf("no cohort %s/%s in %+v", model, version, d.Cohorts)
	return slicediag.Cohort{}
}

// dimOf finds a dimension (and its family) of the cohort or fails.
func dimOf(t *testing.T, c slicediag.Cohort, name string) (slicediag.Family, slicediag.Dimension) {
	t.Helper()
	for _, f := range c.Families {
		for _, d := range f.Dimensions {
			if d.Dimension == name {
				return f, d
			}
		}
	}
	t.Fatalf("no dimension %s in cohort %s/%s", name, c.ModelID, c.QuestionVersion)
	return slicediag.Family{}, slicediag.Dimension{}
}

func bucketOf(t *testing.T, d slicediag.Dimension, name string) slicediag.Bucket {
	t.Helper()
	for _, b := range d.Buckets {
		if b.Bucket == name {
			return b
		}
	}
	t.Fatalf("no bucket %s in %s: %+v", name, d.Dimension, d.Buckets)
	return slicediag.Bucket{}
}
