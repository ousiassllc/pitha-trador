// Package notetests holds the HTTP-level tests of the research-note tools
// (issue #1095): create / list / search / update / get, the versions that stay,
// the optimistic version check, the write switch, the masking of key-shaped
// strings (in the rows, the responses and the audit trail), and that a note
// changes no setting and no trading state. The pure rules (validation, SQL) are
// tested in internal/service/paperresultsmcp/researchnote.
package notetests

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
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/researchnote"
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
}

type response struct {
	status int
	body   []byte
}

// newServer serves the MCP over a migrated database seeded with SeedPositions
// and SeedRisk, with the fixed clock 2026-10-18 12:00 JST. w is a separate
// handle for planting rows and inspecting what was stored.
func newServer(t *testing.T) (*testServer, *sql.DB) { return newServerOn(t, "") }

// newServerOn is newServer with a registered database/sql driver ("" for the
// default), e.g. testkit.NewCountingDriver's, to see the statements run.
func newServerOn(t *testing.T, driverName string) (*testServer, *sql.DB) {
	t.Helper()
	path, w := testkit.NewMigratedDB(t)
	testkit.SeedPositions(t, w)
	testkit.SeedRisk(t, w)
	s := settings.Default()
	s.Enabled = true
	s.APIKey = testAPIKey
	srv := paperresultsmcp.New(paperresultsmcp.Options{
		Settings: s, DBPath: path, DriverName: driverName, Now: testkit.FixedNow, CapitalJPY: 30_000_000,
		Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)),
	})
	hs := httptest.NewServer(srv.Handler())
	t.Cleanup(func() {
		hs.Close()
		_ = srv.Close()
	})
	return &testServer{Server: srv, http: hs}, w
}

// tick moves the writer's clock to at, so the notes written next have distinct
// times (the server's own clock is fixed).
func (s *testServer) tick(t *testing.T, at time.Time) {
	t.Helper()
	w, err := s.Writer()
	if err != nil {
		t.Fatal(err)
	}
	w.SetClock(func() time.Time { return at })
}

func (s *testServer) call(t *testing.T, name string, args any) response {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": name, "arguments": args}})
	req, err := http.NewRequest(http.MethodPost, s.http.URL, strings.NewReader(string(body)))
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

// data decodes a successful response's structuredContent.data into out.
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
		} `json:"result"`
	}
	if err := json.Unmarshal(r.body, &env); err != nil || env.Result.StructuredContent.Tool != tool {
		t.Fatalf("%s: bad envelope %s (%v)", tool, r.body, err)
	}
	if err := json.Unmarshal(env.Result.StructuredContent.Data, out); err != nil {
		t.Fatalf("%s: decode data %s: %v", tool, env.Result.StructuredContent.Data, err)
	}
}

// wantError asserts the HTTP status and the spec error code.
func (r response) wantError(t *testing.T, status int, code paperresultsmcp.Code) {
	t.Helper()
	var e struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(r.body, &e); err != nil || r.status != status || e.Code != string(code) {
		t.Fatalf("want %d %s, got %d %s (%s)", status, code, r.status, r.body, e.Code)
	}
}

func count(t *testing.T, w *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := w.QueryRowContext(t.Context(), query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// create writes a note through the tool and returns it.
func create(t *testing.T, srv *testServer, args map[string]any) researchnote.Note {
	t.Helper()
	var n researchnote.Note
	srv.call(t, "research_note_create", args).data(t, "research_note_create", &n)
	return n
}

func update(t *testing.T, srv *testServer, id int64, version int, extra map[string]any) response {
	t.Helper()
	args := map[string]any{"note_id": id, "expected_version": version}
	for k, v := range extra {
		args[k] = v
	}
	return srv.call(t, "research_note_update", args)
}

func list(t *testing.T, srv *testServer, args map[string]any) []researchnote.Row {
	t.Helper()
	var out struct {
		Notes []researchnote.Row `json:"notes"`
	}
	srv.call(t, "research_notes_list", args).data(t, "research_notes_list", &out)
	return out.Notes
}

func get(t *testing.T, srv *testServer, id int64) researchnote.Detail {
	t.Helper()
	var d researchnote.Detail
	srv.call(t, "research_note_get", map[string]any{"note_id": id}).data(t, "research_note_get", &d)
	return d
}

// noteArgs is a valid research_note_create request; extra overrides.
func noteArgs(question string, extra map[string]any) map[string]any {
	args := map[string]any{
		"question":       question,
		"hypothesis":     "出来高が増えると 30 分後のリターンが改善する",
		"actions":        "research_horizon_summary で 30 分ホライズンを集計した",
		"result_summary": "コスト控除後は改善せず",
		"conclusion":     "unsupported",
		"evidence_links": []map[string]any{{"kind": "experiment_id", "ref": "12"}, {"kind": "url", "ref": "https://example.com/report"}},
		"tags":           []string{"Volume", "horizon"},
	}
	for k, v := range extra {
		args[k] = v
	}
	return args
}
