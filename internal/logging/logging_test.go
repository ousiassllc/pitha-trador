package logging_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/logging"
)

func TestNew_WritesStructuredJSON(t *testing.T) {
	var buf bytes.Buffer
	logger := logging.New(&buf, slog.LevelInfo)

	logger.Info("market data fetch completed", "duration_ms", 42, "error", false)

	line := strings.TrimSpace(buf.String())
	if line == "" {
		t.Fatal("New's logger produced no output")
	}

	var decoded map[string]any
	if err := json.Unmarshal([]byte(line), &decoded); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", err, line)
	}

	if decoded["msg"] != "market data fetch completed" {
		t.Errorf("msg = %v, want %q", decoded["msg"], "market data fetch completed")
	}
	if decoded["duration_ms"] != float64(42) {
		t.Errorf("duration_ms = %v, want 42", decoded["duration_ms"])
	}
}

func TestNew_RespectsLevel(t *testing.T) {
	var buf bytes.Buffer
	logger := logging.New(&buf, slog.LevelWarn)

	logger.Info("should be suppressed")
	if buf.Len() != 0 {
		t.Fatalf("Info logged below configured Warn level: %s", buf.String())
	}

	logger.Warn("should be emitted")
	if buf.Len() == 0 {
		t.Fatal("Warn was not logged at the configured Warn level")
	}
}
