// Package disclosuresrctests holds the HTTP-level tests of the
// disclosure_sources_status tool (issue #1096). They live apart from mcptests,
// which holds the other read tools' tests.
package disclosuresrctests

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/disclosuresrcdiag"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/settings"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

const (
	testAPIKey = "TEST-API-KEY-do-not-leak-0123456789"
	toolName   = "disclosure_sources_status"
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

func newTestServer(t *testing.T, opts paperresultsmcp.Options) *testServer {
	t.Helper()
	s := settings.Default()
	s.Enabled = true
	s.APIKey = testAPIKey
	opts.Settings, opts.CapitalJPY = s, 30_000_000
	opts.Now = func() time.Time { return testkit.FixedNow() }
	opts.Logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
	srv := paperresultsmcp.New(opts)
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

func (s *testServer) callTool(t *testing.T, name string, args any) response {
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

func (s *testServer) call(t *testing.T, args any) response { return s.callTool(t, toolName, args) }

// data decodes the tool's structuredContent.data.
func (r response) data(t *testing.T) disclosuresrcdiag.Data {
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
	var d disclosuresrcdiag.Data
	if err := json.Unmarshal(env.Result.StructuredContent.Data, &d); err != nil {
		t.Fatalf("decode data %s: %v", env.Result.StructuredContent.Data, err)
	}
	return d
}

// period covers the fixture days (JST 2026-10-10 .. 10-17).
var period = map[string]any{"from": "2026-10-10", "to": "2026-10-18"}
