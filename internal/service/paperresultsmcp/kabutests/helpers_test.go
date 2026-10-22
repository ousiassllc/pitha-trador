// Package kabutests checks the kabu alerts (issue #863) end to end through
// the Paper results MCP over HTTP. It is its own package to keep
// mcptests within the linterly directory budget.
package kabutests

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/settings"
)

const testAPIKey = "TEST-API-KEY-do-not-leak-0123456789"

// at is a JST instant on 2026-10-<day>: the 20th is a Tuesday (trading day).
func at(day, hour, minute int) time.Time {
	return time.Date(2026, 10, day, hour, minute, 0, 0, marketcalendar.JST)
}

// callResult is one tools/call exchange: the request and response bodies as
// sent, and the decoded structuredContent.data.
type callResult struct {
	request, response string
	data              json.RawMessage
}

// call invokes tool on the server at url with the right key.
func call(t *testing.T, url, tool string, args any) callResult {
	t.Helper()
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": tool, "arguments": args}})
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
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
	out, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	var env struct {
		Result struct {
			StructuredContent struct {
				Data json.RawMessage `json:"data"`
			} `json:"structuredContent"`
		} `json:"result"`
	}
	if res.StatusCode != http.StatusOK || json.Unmarshal(out, &env) != nil {
		t.Fatalf("%s: status %d, body %s", tool, res.StatusCode, out)
	}
	return callResult{request: string(body), response: string(out), data: env.Result.StructuredContent.Data}
}

// serve starts the MCP server on dbPath, reading now() as the clock.
func serve(t *testing.T, dbPath string, now func() time.Time) string {
	t.Helper()
	s := settings.Default()
	s.Enabled = true
	s.APIKey = testAPIKey
	srv := paperresultsmcp.New(paperresultsmcp.Options{Settings: s, DBPath: dbPath, Now: now, CapitalJPY: 30_000_000})
	hs := httptest.NewServer(srv.Handler())
	t.Cleanup(func() {
		hs.Close()
		_ = srv.Close()
	})
	return hs.URL
}
