package dailybars_test

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	tt "github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana/tachibanatest"
)

// The batch logs counts and duration_ms only: no price or volume value, and
// no per-bar content, reaches the log (#720, non-functional.md §5.1).
func TestLogsCountsAndDurationButNeverPrices(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	f := newFixture(t, tt.AtJST(2026, 10, 8, 18, 0))
	f.addSymbol("1001", "prime", 0, bar("1001", "2026-10-08", 98765.4321))
	f.addSymbol("1002", "prime", 0)
	f.source.errs["1002"] = errString("broker busy")
	f.run(t)

	out := buf.String()
	for _, want := range []string{"nightly daily bars finished", "symbols=2", "requests=2", "saved_bars=1", "failed=1", "duration_ms="} {
		if !strings.Contains(out, want) {
			t.Errorf("log lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "98765") {
		t.Errorf("a price value reached the log:\n%s", out)
	}
}

type errString string

func (e errString) Error() string { return string(e) }
