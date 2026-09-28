package marketdata_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/marketdata"
)

// captureLogs temporarily replaces slog's default logger with one
// writing JSON into the returned buffer, restoring the previous default
// on test cleanup.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return &buf
}

func decodeLogLines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var lines []map[string]any
	for _, raw := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if raw == "" {
			continue
		}
		var decoded map[string]any
		if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
			t.Fatalf("log line is not valid JSON: %v\nline: %s", err, raw)
		}
		lines = append(lines, decoded)
	}
	return lines
}

func TestClient_GetBoard_LogsStructuredLatencyOnSuccess(t *testing.T) {
	buf := captureLogs(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			_ = json.NewEncoder(w).Encode(map[string]any{"ResultCode": 0, "Token": "tok"})
		case "/board/7203@1":
			_ = json.NewEncoder(w).Encode(map[string]any{"Symbol": "7203", "CurrentPrice": 1234.5})
		}
	}))
	defer server.Close()

	client := marketdata.NewClient(marketdata.Config{BaseURL: server.URL, APIPassword: "pw"})
	if _, err := client.IssueToken(context.Background()); err != nil {
		t.Fatalf("IssueToken: %v", err)
	}
	if _, err := client.GetBoard(context.Background(), "7203", marketdata.ExchangeTSE); err != nil {
		t.Fatalf("GetBoard: %v", err)
	}

	found := false
	for _, line := range decodeLogLines(t, buf) {
		if line["msg"] == "marketdata: api call completed" && line["path"] == "/board/7203@1" {
			found = true
			if _, ok := line["duration_ms"]; !ok {
				t.Errorf("log line missing duration_ms: %v", line)
			}
			if _, hasError := line["error"]; hasError {
				t.Errorf("successful call's log line should not carry an error field: %v", line)
			}
		}
	}
	if !found {
		t.Fatalf("no structured log line for GetBoard's call; lines: %+v", decodeLogLines(t, buf))
	}
}

func TestClient_GetBoard_LogsErrorOnFailure(t *testing.T) {
	buf := captureLogs(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			_ = json.NewEncoder(w).Encode(map[string]any{"ResultCode": 0, "Token": "tok"})
		case "/board/7203@1":
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]any{"Code": 4001004, "Message": "board error"})
		}
	}))
	defer server.Close()

	client := marketdata.NewClient(marketdata.Config{BaseURL: server.URL, APIPassword: "pw"})
	if _, err := client.IssueToken(context.Background()); err != nil {
		t.Fatalf("IssueToken: %v", err)
	}
	if _, err := client.GetBoard(context.Background(), "7203", marketdata.ExchangeTSE); err == nil {
		t.Fatal("GetBoard should have failed on a 500 response")
	}

	found := false
	for _, line := range decodeLogLines(t, buf) {
		if line["msg"] == "marketdata: api call failed" && line["path"] == "/board/7203@1" {
			found = true
			if line["error"] == nil || line["error"] == "" {
				t.Errorf("failed call's log line should carry a non-empty error field: %v", line)
			}
		}
	}
	if !found {
		t.Fatalf("no structured error log line for GetBoard's failed call; lines: %+v", decodeLogLines(t, buf))
	}
}
