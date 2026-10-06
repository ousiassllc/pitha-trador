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

func TestNewSplit_ErrorsGoToBothSinksOthersOnlyToFullLog(t *testing.T) {
	var all, errs bytes.Buffer
	logger := logging.NewSplit(&all, &errs, slog.LevelInfo).With("component", "x")

	logger.Debug("below level")
	logger.Info("an info", "k", 1)
	logger.Warn("a warning")
	logger.Error("an error", "error", "boom")

	for _, msg := range []string{"an info", "a warning", "an error"} {
		if !strings.Contains(all.String(), msg) {
			t.Errorf("full log lacks %q:\n%s", msg, all.String())
		}
	}
	if strings.Contains(all.String(), "below level") {
		t.Errorf("full log contains a record below its level:\n%s", all.String())
	}
	if !strings.Contains(errs.String(), "an error") || !strings.Contains(errs.String(), `"component":"x"`) {
		t.Errorf("error log lacks the ERROR record with its With-attrs:\n%s", errs.String())
	}
	if strings.Contains(errs.String(), "an info") || strings.Contains(errs.String(), "a warning") {
		t.Errorf("error log contains non-ERROR records:\n%s", errs.String())
	}
}
