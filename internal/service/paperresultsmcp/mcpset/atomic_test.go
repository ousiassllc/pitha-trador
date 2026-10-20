package mcpset_test

import (
	"context"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/papersettings"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

// failInserts makes every INSERT into table fail, the way a full disk or a
// constraint violation on that one table would (issue #813).
func failInserts(t *testing.T, f *fixture, table string) {
	t.Helper()
	testkit.MustExec(t, f.db, `CREATE TRIGGER fail_`+table+`_insert BEFORE INSERT ON `+table+
		` BEGIN SELECT RAISE(ABORT, 'injected failure'); END`)
}

func (f *fixture) count(t *testing.T, table string) int {
	t.Helper()
	var n int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// The Settings screen's save and its audit row are one transaction: when the
// audit row cannot be written the saved value is rolled back and the save
// fails, so a setting never changes without a trace.
func TestGUISaveRollsBackWhenTheAuditRowFails(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	key := papersettings.KeyMaxTotalExposurePct
	if err := f.ops.Save(ctx, key, "70"); err != nil {
		t.Fatal(err)
	}
	failInserts(t, f, "mcp_settings_audit")

	if err := f.ops.Save(ctx, key, "60"); err == nil {
		t.Fatal("Save succeeded although its audit row failed")
	}
	if v, err := f.ops.Get(ctx, key); err != nil || v.Current != "70" {
		t.Fatalf("value after the failed save = %+v, %v; want the previous 70", v, err)
	}
	if err := f.ops.Reset(ctx, key); err == nil {
		t.Fatal("Reset succeeded although its audit row failed")
	}
	if v, err := f.ops.Get(ctx, key); err != nil || !v.Overridden || v.Current != "70" {
		t.Fatalf("value after the failed reset = %+v, %v; want the stored 70", v, err)
	}
	if n := f.count(t, "mcp_settings_audit"); n != 1 {
		t.Fatalf("audit rows = %d, want only the first save's", n)
	}
}

// The other direction: a setting that cannot be stored leaves no audit row.
func TestGUISaveLeavesNoAuditRowWhenTheSettingFails(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	failInserts(t, f, "runtime_settings")

	if err := f.ops.Save(ctx, papersettings.KeyMaxTotalExposurePct, "60"); err == nil {
		t.Fatal("Save succeeded although the setting could not be stored")
	}
	if n := f.count(t, "mcp_settings_audit"); n != 0 {
		t.Fatalf("audit rows = %d after a failed save, want 0", n)
	}
}

// The MCP's write path has the same guarantee (issue #806): Set and SetEnabled
// store the value and the audit row in one transaction.
func TestMCPWriterRollsBackWhenTheAuditRowFails(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	failInserts(t, f, "mcp_settings_audit")

	if _, err := f.writer.Set(ctx, "c", papersettings.KeyMaxTotalExposurePct, "60"); err == nil {
		t.Fatal("Set succeeded although its audit row failed")
	}
	if _, err := f.writer.SetEnabled(ctx, "c", false); err == nil {
		t.Fatal("SetEnabled succeeded although its audit row failed")
	}
	if err := f.gate.SetEnabled(ctx, false); err == nil {
		t.Fatal("the GUI toggle succeeded although its audit row failed")
	}
	if n := f.count(t, "runtime_settings"); n != 0 {
		t.Fatalf("runtime_settings rows = %d, want none: every write rolled back", n)
	}
	if on, err := f.gate.Enabled(ctx); err != nil || !on {
		t.Fatalf("write switch = %v, %v; want the untouched default (on)", on, err)
	}
}

func TestMCPWriterLeavesNoAuditRowWhenTheSettingFails(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	failInserts(t, f, "runtime_settings")

	if _, err := f.writer.Set(ctx, "c", papersettings.KeyMaxTotalExposurePct, "60"); err == nil {
		t.Fatal("Set succeeded although the setting could not be stored")
	}
	if _, err := f.writer.SetEnabled(ctx, "c", false); err == nil {
		t.Fatal("SetEnabled succeeded although the setting could not be stored")
	}
	if n := f.count(t, "mcp_settings_audit"); n != 0 {
		t.Fatalf("audit rows = %d, want 0", n)
	}
}
