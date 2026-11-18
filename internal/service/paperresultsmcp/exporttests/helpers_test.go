// Package exporttests holds the HTTP-level tests of the training_dataset_export tool (issue
// #1094): the training dataset it writes to the local exports folder and the manifest it
// returns. All data is synthetic (a migrated temp SQLite file); the files are written under the
// test's temp dir, next to the database file.
package exporttests

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/datasetexport"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/settings"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

const (
	testAPIKey = "TEST-API-KEY-do-not-leak-0123456789"
	tool       = "training_dataset_export"
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
	// dbDir is the folder of the database file: the exports folder is dbDir/exports.
	dbDir string
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
		Now: testkit.FixedNow, CapitalJPY: 30_000_000,
		// The scale test exports MaxDecisions decisions; under -race on a loaded CI runner that exceeds the
		// production 3 s budget, which is not what these tests pin (the limits and the files are).
		QueryTimeout: time.Minute,
	})
	hs := httptest.NewServer(srv.Handler())
	t.Cleanup(func() {
		hs.Close()
		_ = srv.Close()
	})
	return &testServer{Server: srv, http: hs, dbDir: filepath.Dir(path)}, w
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

// The period of the fixture: 2026-10-05 .. 2026-10-12 (JST, to exclusive).
const (
	periodFrom = "2026-10-05"
	periodTo   = "2026-10-12"
)

func (s *testServer) call(t *testing.T, extra map[string]any) response {
	t.Helper()
	args := map[string]any{"from": periodFrom, "to": periodTo}
	for k, v := range extra {
		args[k] = v
	}
	return s.post(t, "tools/call", map[string]any{"name": tool, "arguments": args})
}

// export decodes the successful tool response's structuredContent.data.
func (r response) export(t *testing.T) datasetexport.Response {
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
	var d datasetexport.Response
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

// enableExport turns the export switch on the way Settings does.
func enableExport(t *testing.T, w *sql.DB) {
	t.Helper()
	testkit.MustExec(t, w, `INSERT OR REPLACE INTO runtime_settings (key, value, updated_at) VALUES ('research.export.enabled', '1', 't')`)
}

// exportsDir is where the tool writes (the test computes it itself, never from the response).
func (s *testServer) exportsDir() string { return filepath.Join(s.dbDir, "exports") }

func sha(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
