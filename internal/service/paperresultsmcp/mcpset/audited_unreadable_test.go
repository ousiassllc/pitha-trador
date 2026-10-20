package mcpset_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/papersettings"
)

// captureWarnings routes slog to a buffer for the test and restores the
// previous default afterwards.
func captureWarnings(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

// corruptStoredValue overwrites key's stored row with text that is not a JSON
// number, so reading the value back fails.
func corruptStoredValue(t *testing.T, f *fixture, key string) {
	t.Helper()
	if _, err := f.db.Exec(`INSERT INTO runtime_settings (key, value, updated_at) VALUES (?, 'not-a-number', '2026-10-18T00:00:00Z')
ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key); err != nil {
		t.Fatal(err)
	}
}

// A stored value that cannot be read before the change (a damaged row) must
// not stop the operator from overwriting or resetting it from the Settings
// screen: the change goes through, no audit row is written, and a warning is
// logged. This pins the documented exception of Audited.audited (issue #813).
func TestGUIChangeOfAnUnreadableStoredValueSucceedsWithoutAuditRow(t *testing.T) {
	ctx := context.Background()
	key := papersettings.KeyMaxTotalExposurePct

	t.Run("save", func(t *testing.T) {
		f := newFixture(t)
		logs := captureWarnings(t)
		corruptStoredValue(t, f, key)
		if _, err := f.ops.Get(ctx, key); err == nil {
			t.Fatal("setup: the damaged row is readable")
		}

		if err := f.ops.Save(ctx, key, "60"); err != nil {
			t.Fatalf("Save of a damaged row failed: %v", err)
		}
		if v, err := f.ops.Get(ctx, key); err != nil || !v.Overridden || v.Current != "60" {
			t.Fatalf("value after the save = %+v, %v; want the new 60", v, err)
		}
		if n := f.count(t, "mcp_settings_audit"); n != 0 {
			t.Fatalf("audit rows = %d, want 0 (the previous value was unreadable)", n)
		}
		if out := logs.String(); !strings.Contains(out, "read value before change") || !strings.Contains(out, key) {
			t.Fatalf("no warning about the unreadable value for %s; log: %q", key, out)
		}
	})

	t.Run("reset", func(t *testing.T) {
		f := newFixture(t)
		logs := captureWarnings(t)
		corruptStoredValue(t, f, key)

		if err := f.ops.Reset(ctx, key); err != nil {
			t.Fatalf("Reset of a damaged row failed: %v", err)
		}
		if v, err := f.ops.Get(ctx, key); err != nil || v.Overridden {
			t.Fatalf("value after the reset = %+v, %v; want the default (not stored)", v, err)
		}
		if n := f.count(t, "mcp_settings_audit"); n != 0 {
			t.Fatalf("audit rows = %d, want 0 (the previous value was unreadable)", n)
		}
		if out := logs.String(); !strings.Contains(out, "read value before change") || !strings.Contains(out, key) {
			t.Fatalf("no warning about the unreadable value for %s; log: %q", key, out)
		}
	})
}
