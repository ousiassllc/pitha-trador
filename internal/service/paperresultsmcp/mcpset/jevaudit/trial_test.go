package jevaudit_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/jevtrialsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/jevtrial"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/mcpset"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/mcpset/jevaudit"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

const orArmJSON = `[{"id":"or-a","provider":"openrouter","model":"vendor/model-a","prompt_version":"trader-v3"}]`

func orArms() []jevtrialsettings.Arm {
	return []jevtrialsettings.Arm{{ID: "or-a", Provider: "openrouter", Model: "vendor/model-a", PromptVersion: "trader-v3"}}
}

func newTrial(t *testing.T) (*jevaudit.Trial, *sql.DB, string) {
	t.Helper()
	path, db := testkit.NewMigratedDB(t)
	svc := jevaudit.NewTrial(jevtrial.New(db), db)
	svc.SetClock(func() time.Time { return savedAt })
	return svc, db, path
}

func capOf(v float64) *float64 { return &v }

// Issue #1220: a Settings-screen save of the trial form leaves one mcp_settings_audit row
// (actor gui) for each of the six keys whose value changed, and none for the others.
func TestJevTrialSettings_ScreenSaveWritesAudit(t *testing.T) {
	svc, db, _ := newTrial(t)
	err := svc.Save(context.Background(), jevtrial.Form{Enabled: true, Arms: orArms(), Cap: capOf(0.25)})
	if err != nil {
		t.Fatal(err)
	}
	wantRows(t, db,
		row{actor: "gui", action: "set", key: "jev.trial.enabled", after: str("true")},
		row{actor: "gui", action: "set", key: "jev.trial.arms", after: str(orArmJSON)},
		row{actor: "gui", action: "set", key: "jev.trial.daily_cost_cap_usd", after: str("0.25")},
	)

	// The form sends every field on every save: the same form again changes nothing and records nothing.
	if err := svc.Save(context.Background(), jevtrial.Form{Enabled: true, Arms: orArms(), Cap: capOf(0.25)}); err != nil {
		t.Fatal(err)
	}
	if n := len(audit(t, db)); n != 3 {
		t.Errorf("an unchanged save added %d audit rows", n-3)
	}

	// Codex switch and caps, then the arm of that provider.
	calls, tokens := 20, 100000
	form := jevtrial.Form{Enabled: true, Arms: orArms(), Cap: capOf(0.25), CodexEnabled: true, CodexCallCap: &calls, CodexTokenCap: &tokens}
	if err := svc.Save(context.Background(), form); err != nil {
		t.Fatal(err)
	}
	got := audit(t, db)[3:]
	wantKeys := []string{"jev.trial.codex.enabled", "jev.trial.codex.daily_call_cap", "jev.trial.codex.daily_token_cap"}
	if len(got) != len(wantKeys) {
		t.Fatalf("codex save rows = %+v", got)
	}
	for i, k := range wantKeys {
		// The first save of the form stored the Codex switch as 0 (off), so its before is "false";
		// the two caps had nothing stored.
		if got[i].actor != "gui" || got[i].key != k || (i == 0) != (got[i].before != nil) {
			t.Errorf("row %d = %+v, want a gui row of %s", i, got[i], k)
		}
	}
	if *got[0].before != "false" {
		t.Errorf("codex switch before = %q, want false", *got[0].before)
	}
	if *got[0].after != "true" || *got[1].after != "20" || *got[2].after != "100000" {
		t.Errorf("codex rows after = %q %q %q", *got[0].after, *got[1].after, *got[2].after)
	}
}

// A blank cap field removes the stored value: recorded as a reset with the value it had; a
// blank one while nothing is stored records nothing.
func TestJevTrialSettings_BlankCapIsRecordedAsReset(t *testing.T) {
	svc, db, _ := newTrial(t)
	ctx := context.Background()
	if err := svc.Save(ctx, jevtrial.Form{Arms: orArms()}); err != nil { // no cap stored, none given
		t.Fatal(err)
	}
	for _, r := range audit(t, db) {
		if r.key == "jev.trial.daily_cost_cap_usd" {
			t.Fatalf("a blank cap with nothing stored was recorded: %+v", r)
		}
	}
	if err := svc.Save(ctx, jevtrial.Form{Arms: orArms(), Cap: capOf(0.3)}); err != nil {
		t.Fatal(err)
	}
	if err := svc.Save(ctx, jevtrial.Form{Arms: orArms()}); err != nil {
		t.Fatal(err)
	}
	rows := audit(t, db)
	last := rows[len(rows)-1]
	if last.actor != "gui" || last.action != "reset" || last.key != "jev.trial.daily_cost_cap_usd" || last.before == nil || *last.before != "0.3" || last.after != nil {
		t.Errorf("last row = %+v, want a gui reset of the cap from 0.3", last)
	}
	if _, ok := stored(t, db, "jev.trial.daily_cost_cap_usd"); ok {
		t.Error("the cap is still stored after the reset")
	}
}

// A form the service rejects stores no setting and no audit row, even when an earlier
// field of it was fine.
func TestJevTrialSettings_RejectedSaveLeavesNoTrace(t *testing.T) {
	svc, db, _ := newTrial(t)
	twice := append(orArms(), orArms()...)
	for name, form := range map[string]jevtrial.Form{
		"duplicate arms":      {Enabled: true, Arms: twice, Cap: capOf(0.2)},
		"cap above maximum":   {Enabled: true, Arms: orArms(), Cap: capOf(1000)},
		"codex arm, no codex": {Enabled: true, Arms: []jevtrialsettings.Arm{{ID: "cx", Provider: "codex_chatgpt", Model: "m", PromptVersion: "trader-v3"}}},
	} {
		if err := svc.Save(context.Background(), form); !errors.Is(err, jevtrial.ErrInvalidInput) {
			t.Errorf("%s: error = %v, want ErrInvalidInput", name, err)
		}
	}
	wantRows(t, db)
	for _, key := range jevtrialsettings.OperatorKeys() {
		if v, ok := stored(t, db, key); ok {
			t.Errorf("a rejected save stored %s = %s", key, v)
		}
	}
}

// What the screen saves and what the MCP writes are one value: the MCP's before is the
// screen's after, and the screen's service reads what the MCP wrote.
func TestJevTrialSettings_ScreenAndMCPSeeTheSameValues(t *testing.T) {
	svc, db, path := newTrial(t)
	ctx := context.Background()
	if err := svc.Save(ctx, jevtrial.Form{Enabled: true, Arms: orArms(), Cap: capOf(0.25)}); err != nil {
		t.Fatal(err)
	}
	w, err := mcpset.OpenWriter("sqlite", path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = w.Close() }()
	w.SetClock(func() time.Time { return savedAt })
	e, err := w.Set(ctx, "test-client", "jev.trial.daily_cost_cap_usd", "0.4")
	if err != nil {
		t.Fatal(err)
	}
	if e.Actor != "mcp" || e.Before == nil || *e.Before != "0.25" || e.After == nil || *e.After != "0.4" {
		t.Errorf("MCP audit entry = %+v, want before 0.25 (the screen's value) and after 0.4", e)
	}
	st, err := svc.State(ctx)
	if err != nil || st.CapUSD != 0.4 || st.CapIsDefault || !st.Enabled || len(st.Arms) != 1 {
		t.Errorf("screen state after the MCP write = %+v (%v)", st, err)
	}
	wantTail := row{actor: "mcp", action: "set", key: "jev.trial.daily_cost_cap_usd", before: str("0.25"), after: str("0.4")}
	rows := audit(t, db)
	if got := rows[len(rows)-1]; got.actor != wantTail.actor || got.key != wantTail.key || !eq(got.before, wantTail.before) || !eq(got.after, wantTail.after) {
		t.Errorf("last audit row = %+v", got)
	}
}
