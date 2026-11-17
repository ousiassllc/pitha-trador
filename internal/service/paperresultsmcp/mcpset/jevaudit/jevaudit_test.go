package jevaudit_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/jevshadowsettings"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/service/jevprompt"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/mcpset"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/mcpset/jevaudit"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

func TestMain(m *testing.M) {
	code := m.Run()
	testkit.RemoveTemplate()
	os.Exit(code)
}

var savedAt = time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)

func newService(t *testing.T) (*jevaudit.Service, *sql.DB) {
	t.Helper()
	_, db := testkit.NewMigratedDB(t)
	svc := jevaudit.New(jevprompt.New(db), db)
	svc.SetClock(func() time.Time { return savedAt })
	return svc, db
}

// row is one mcp_settings_audit row as the tests compare it.
type row struct {
	actor, action, key string
	before, after      *string
}

func str(s string) *string { return &s }

func audit(t *testing.T, db *sql.DB) []row {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), `SELECT actor, action, setting_key, before_value, after_value FROM mcp_settings_audit ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []row
	for rows.Next() {
		var r row
		var b, a sql.NullString
		if err := rows.Scan(&r.actor, &r.action, &r.key, &b, &a); err != nil {
			t.Fatal(err)
		}
		if b.Valid {
			r.before = &b.String
		}
		if a.Valid {
			r.after = &a.String
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func stored(t *testing.T, db *sql.DB, key string) (string, bool) {
	t.Helper()
	v, ok, err := system.NewRuntimeSettingsRepository(db).Get(t.Context(), key)
	if err != nil {
		t.Fatal(err)
	}
	return v, ok
}

func eq(a, b *string) bool { return (a == nil && b == nil) || (a != nil && b != nil && *a == *b) }

func wantRows(t *testing.T, db *sql.DB, want ...row) {
	t.Helper()
	got := audit(t, db)
	if len(got) != len(want) {
		t.Fatalf("audit rows = %d, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		g, w := got[i], want[i]
		if g.actor != w.actor || g.action != w.action || g.key != w.key || !eq(g.before, w.before) || !eq(g.after, w.after) {
			t.Errorf("audit row %d = %+v, want %+v", i, g, w)
		}
	}
}

// Issue #1113: a Settings-screen save of either shadow key leaves one row in
// mcp_settings_audit (the MCP's table) with actor gui, the key and the values
// before and after, in the form settings_get / settings_set use.
func TestJevPromptSettings_ScreenSaveWritesAudit(t *testing.T) {
	svc, db := newService(t)
	ctx := context.Background()
	if err := svc.SetCandidate(ctx, "trader-v4"); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetEnabled(ctx, true); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetEnabled(ctx, false); err != nil {
		t.Fatal(err)
	}
	wantRows(t, db,
		row{mcpset.ActorGUI, mcpset.ActionSet, jevshadowsettings.KeyCandidateVersion, nil, str("trader-v4")},
		row{mcpset.ActorGUI, mcpset.ActionSet, jevshadowsettings.KeyEnabled, nil, str("true")},
		row{mcpset.ActorGUI, mcpset.ActionSet, jevshadowsettings.KeyEnabled, str("true"), str("false")},
	)
	if v, _ := stored(t, db, jevshadowsettings.KeyEnabled); v != "0" {
		t.Errorf("stored switch = %q, want 0 (jevprompt.Service's own format)", v)
	}
}

func TestJevPromptSettings_ResetWritesAudit(t *testing.T) {
	svc, db := newService(t)
	ctx := context.Background()
	if err := svc.SetCandidate(ctx, "trader-v4"); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetCandidate(ctx, ""); err != nil {
		t.Fatal(err)
	}
	// Clearing a candidate that is not stored changes nothing: no row.
	if err := svc.SetCandidate(ctx, ""); err != nil {
		t.Fatal(err)
	}
	wantRows(t, db,
		row{mcpset.ActorGUI, mcpset.ActionSet, jevshadowsettings.KeyCandidateVersion, nil, str("trader-v4")},
		row{mcpset.ActorGUI, mcpset.ActionReset, jevshadowsettings.KeyCandidateVersion, str("trader-v4"), nil},
	)
	if _, ok := stored(t, db, jevshadowsettings.KeyCandidateVersion); ok {
		t.Error("the candidate is still stored after the reset")
	}
}

// A rejected save (an unregistered version, the live version) stores neither
// the setting nor an audit row; jevprompt.Service's validation is unchanged.
func TestJevPromptSettings_RejectedSaveWritesNothing(t *testing.T) {
	svc, db := newService(t)
	ctx := context.Background()
	for name, version := range map[string]string{"unregistered": "trader-v999", "the live version": "trader-v3"} {
		t.Run(name, func(t *testing.T) {
			err := svc.SetCandidate(ctx, version)
			if !errors.Is(err, jevprompt.ErrInvalidInput) {
				t.Fatalf("SetCandidate(%q) = %v, want ErrInvalidInput", version, err)
			}
			if _, ok := stored(t, db, jevshadowsettings.KeyCandidateVersion); ok {
				t.Error("a rejected candidate was stored")
			}
			wantRows(t, db)
		})
	}
}

// The Settings form sends the switch and the candidate on every save, also
// when only the cost cap changed. A value saved as it already is is not a
// change: no row, so an experiment does not see a switch (or a confounder)
// that never happened.
func TestJevPromptSettings_UnchangedSaveWritesNoAudit(t *testing.T) {
	svc, db := newService(t)
	ctx := context.Background()
	for range 2 {
		if err := svc.SetEnabled(ctx, false); err != nil { // the default, unchecked
			t.Fatal(err)
		}
	}
	if err := svc.SetCandidate(ctx, "trader-v4"); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetCandidate(ctx, "trader-v4"); err != nil {
		t.Fatal(err)
	}
	wantRows(t, db, row{mcpset.ActorGUI, mcpset.ActionSet, jevshadowsettings.KeyCandidateVersion, nil, str("trader-v4")})
}

// The cost cap is not one of the two audited keys.
func TestJevPromptSettings_CostCapIsNotAudited(t *testing.T) {
	svc, db := newService(t)
	if err := svc.SetCap(t.Context(), 0.5); err != nil {
		t.Fatal(err)
	}
	if err := svc.ResetCap(t.Context()); err != nil {
		t.Fatal(err)
	}
	wantRows(t, db)
}

// The setting and its audit row are one transaction: when the audit row
// cannot be written, the save is rolled back and an error is returned.
func TestJevPromptSettings_AuditFailureRollsTheSaveBack(t *testing.T) {
	svc, db := newService(t)
	testkit.MustExec(t, db, `DROP TABLE mcp_settings_audit`)
	if err := svc.SetEnabled(t.Context(), true); err == nil {
		t.Fatal("SetEnabled succeeded without an audit table")
	}
	if _, ok := stored(t, db, jevshadowsettings.KeyEnabled); ok {
		t.Error("the switch was stored although its audit row failed")
	}
}
