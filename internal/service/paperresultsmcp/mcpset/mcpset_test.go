package mcpset_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/config/mcpwritesettings"
	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/papersettings"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/service/opsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/mcpset"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

func TestMain(m *testing.M) {
	code := m.Run()
	testkit.RemoveTemplate()
	os.Exit(code)
}

type fixture struct {
	db     *sql.DB
	path   string
	writer *mcpset.Writer
	gate   *mcpset.Gate
	ops    *mcpset.Audited // the application's side: opsettings with the audit trail
	now    time.Time
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	path, db := testkit.NewMigratedDB(t)
	f := &fixture{db: db, path: path, now: time.Date(2026, 10, 18, 3, 0, 0, 0, time.UTC)}
	w, err := mcpset.OpenWriter("sqlite", path, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	w.SetClock(func() time.Time { return f.now })
	f.writer = w
	f.gate = mcpset.NewGate(db)
	f.ops = mcpset.NewAudited(opsettings.New(system.NewRuntimeSettingsRepository(db), config.StrategyConfig{}, "").WithTradingMode(metalabelsettings.ModePaper), f.gate)
	return f
}

// The Settings screen and the MCP share the store: a value saved by the GUI is
// what the MCP reads and diffs, and a value set from the MCP is what the GUI
// service reads - and the GUI's change is audited like the MCP's.
func TestGUIAndMCPShareTheStoreAndTheAuditLog(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	key := papersettings.KeyMaxTotalExposurePct

	if _, err := f.writer.Set(ctx, "10.0.0.5", key, "80"); err != nil {
		t.Fatal(err)
	}
	v, err := f.ops.Get(ctx, key)
	if err != nil || v.Current != "80" || !v.Overridden {
		t.Fatalf("GUI reads %+v, %v after an MCP write", v, err)
	}

	if err := f.ops.Save(ctx, key, "60"); err != nil { // the Settings screen saves
		t.Fatal(err)
	}
	st, err := mcpset.Current(ctx, f.db, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range st.Items {
		if it.Key == key && (it.Value != "60" || it.Source != mcpset.SourceStored) {
			t.Fatalf("MCP reads %+v after a GUI save", it)
		}
	}
	if err := f.ops.Reset(ctx, key); err != nil { // and resets it
		t.Fatal(err)
	}
	if err := f.ops.Reset(ctx, key); err != nil { // a second reset changes nothing
		t.Fatal(err)
	}

	rows, err := mcpset.ListAudit(ctx, f.db, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("audit rows = %+v, want MCP set, GUI set, GUI reset (not the no-op reset)", rows)
	}
	reset, gui, mcp := rows[0], rows[1], rows[2]
	if mcp.Actor != "mcp" || mcp.Client != "10.0.0.5" || mcp.Before != nil || *mcp.After != "80" {
		t.Errorf("mcp row = %+v", mcp)
	}
	if gui.Actor != "gui" || gui.Client != "" || gui.Action != "set" || *gui.Before != "80" || *gui.After != "60" {
		t.Errorf("gui row = %+v", gui)
	}
	if reset.Action != "reset" || *reset.Before != "60" || reset.After != nil {
		t.Errorf("reset row = %+v", reset)
	}

	// The diff since the beginning sees the end state (back at default): no change.
	d, err := mcpset.DiffSince(ctx, f.db, nil, mcpset.Snapshot{ID: new(int64(0))})
	if err != nil || len(d.Changes) != 0 {
		t.Fatalf("diff after a round trip = %+v, %v", d, err)
	}
}

// Changes of keys the MCP may not write are not audited (the log is the
// MCP's: whitelisted keys and the write switch only).
func TestGUIChangesOfOtherKeysAreNotAudited(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	if err := f.ops.Save(ctx, "paper.bootstrap_entry_enabled", "1"); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if got, err := mcpset.LatestAuditID(ctx, f.db); err != nil || got != 0 {
		t.Fatalf("latest audit id = %d, %v", got, err)
	}
}

func TestWriterRejectsBeforeTouchingAnything(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	if _, err := f.writer.Set(ctx, "c", "paper.bootstrap_entry_enabled", "1"); !errors.Is(err, mcpset.ErrKeyNotAllowed) {
		t.Fatalf("non-whitelisted key: %v", err)
	}
	if _, err := f.writer.SetEnabled(ctx, "c", false); err != nil {
		t.Fatal(err)
	}
	if _, err := f.writer.Set(ctx, "c", papersettings.KeyMaxTradeLossPct, "2"); !errors.Is(err, mcpset.ErrWriteDisabled) {
		t.Fatalf("writes off: %v", err)
	}
	_, err := f.writer.Set(ctx, "c", "risk.paper.max_trade_loss_pct", "9")
	if !errors.Is(err, mcpset.ErrWriteDisabled) { // the switch is checked before the value
		t.Fatalf("writes off, bad value: %v", err)
	}
	if _, err := f.writer.SetEnabled(ctx, "c", true); err != nil {
		t.Fatal(err)
	}
	_, err = f.writer.Set(ctx, "c", papersettings.KeyMaxTradeLossPct, "9")
	if reason, ok := mcpset.IsInvalidValue(err); !ok || reason == "" {
		t.Fatalf("out of range: %v", err)
	}
	if _, err := f.writer.Set(ctx, "c", papersettings.KeyMaxTradeLossPct, "  "); err == nil {
		t.Fatal("a blank value was accepted")
	}
	var n int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM runtime_settings WHERE key = ?`, papersettings.KeyMaxTradeLossPct).Scan(&n); err != nil || n != 0 {
		t.Fatalf("rejected writes stored %d rows, %v", n, err)
	}
}

// The writer never creates the database or migrates: a missing file is
// ErrUnavailable and stays missing.
func TestOpenWriterNeverCreatesTheFile(t *testing.T) {
	missing := t.TempDir() + "/none.db"
	if _, err := mcpset.OpenWriter("sqlite", missing, nil); !errors.Is(err, mcpset.ErrUnavailable) {
		t.Fatalf("missing file: %v", err)
	}
	if _, err := mcpset.OpenWriter("sqlite", t.TempDir(), nil); !errors.Is(err, mcpset.ErrUnavailable) {
		t.Fatalf("directory: %v", err)
	}
}

// The Settings toggle: stored under the shared key, default on, audited as a
// GUI change; the MCP writer sees it.
func TestGateSwitch(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	if on, err := f.gate.Enabled(ctx); err != nil || !on {
		t.Fatalf("default = %v, %v; writes are on while nothing is stored", on, err)
	}
	if err := f.gate.SetEnabled(ctx, false); err != nil {
		t.Fatal(err)
	}
	if on, _ := f.gate.Enabled(ctx); on {
		t.Fatal("still on after SetEnabled(false)")
	}
	if _, err := f.writer.Set(ctx, "c", papersettings.KeyMaxTradeLossPct, "2"); !errors.Is(err, mcpset.ErrWriteDisabled) {
		t.Fatalf("the MCP writer ignored the GUI's switch: %v", err)
	}
	rows, err := f.gate.Recent(ctx, 5)
	if err != nil || len(rows) != 1 {
		t.Fatalf("recent = %+v, %v", rows, err)
	}
	if r := rows[0]; r.Actor != "gui" || r.Action != "write_disable" || r.Key != mcpwritesettings.KeyWriteEnabled || *r.Before != "1" || *r.After != "0" {
		t.Fatalf("row = %+v", r)
	}
}

// Without a stored value the Paper capital falls back; a stored one wins; a
// damaged row is ignored like the Risk Engine ignores it.
func TestPaperCapitalJPY(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	get := func() float64 {
		t.Helper()
		v, err := mcpset.PaperCapitalJPY(ctx, f.db, 123)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	if get() != 123 {
		t.Fatal("no stored value: want the fallback")
	}
	testkit.MustExec(t, f.db, `INSERT INTO runtime_settings (key, value, updated_at) VALUES (?, '750000', 't')`, papersettings.KeyInitialCapitalJPY)
	if get() != 750000 {
		t.Fatal("a stored value must win")
	}
	testkit.MustExec(t, f.db, `UPDATE runtime_settings SET value = '"x"' WHERE key = ?`, papersettings.KeyInitialCapitalJPY)
	if get() != 123 {
		t.Fatal("a damaged row must fall back")
	}
}

// Every whitelisted key is one the application's opsettings manages, so a
// GUI-visible validation exists for it (the MCP never invents its own).
func TestWhitelistedKeysAreOpsettingsKeys(t *testing.T) {
	for _, key := range mcpwritesettings.Keys() {
		if !opsettings.IsKnownKey(key) {
			t.Errorf("%s is not an opsettings key", key)
		}
	}
}
