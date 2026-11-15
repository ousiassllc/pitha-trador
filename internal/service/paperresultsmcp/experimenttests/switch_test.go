package experimenttests

import (
	"database/sql"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/experiment"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

// 切り替えの検証 (issue #879): an experiment is judged only when the variable is
// on record as having gone from the declared before to the declared after at
// the declared start. A different switch, no switch and nothing on record all
// withhold the verdict (switch_unconfirmed), however good the numbers look.

const key = "risk.paper.max_trade_loss_pct"

// wantSwitch asserts the comparison of a clear win that is not judged because
// the switch check says status.
func wantSwitch(t *testing.T, c experiment.Comparison, status string) {
	t.Helper()
	if c.Switch.Status != status || c.Switch.Detail == "" {
		t.Fatalf("switch check = %+v, want status %s with a detail", c.Switch, status)
	}
	if status == experiment.SwitchConfirmed {
		if c.Verdict != experiment.VerdictImproved || !c.Judged {
			t.Fatalf("confirmed switch: verdict %s judged=%v, want improved", c.Verdict, c.Judged)
		}
		return
	}
	if c.Verdict != experiment.VerdictSwitchUnconfirmed || c.Judged {
		t.Fatalf("verdict %s judged=%v, want switch_unconfirmed and no verdict", c.Verdict, c.Judged)
	}
	if c.Confounded {
		t.Errorf("an unconfirmed switch is not a confounder: %+v", c.Confounders)
	}
	if c.After.Performance.ClosedTradeCount != 64 || c.PerTradePnl.CILow == nil {
		t.Errorf("the figures must still be returned: after=%+v test=%+v", c.After.Performance, c.PerTradePnl)
	}
}

// settingSwitch plants a clear win and a closed setting experiment whose audit
// trail is built by the caller (createUnrecorded plants none).
func settingSwitch(t *testing.T, extra map[string]any) (*testServer, *sql.DB, int64) {
	t.Helper()
	srv, w := newServer(t)
	plantImproved(t, w)
	e := createUnrecorded(t, srv, createArgs(key, extra))
	closeAt(t, srv, e.ID, endedAt)
	return srv, w, e.ID
}

func TestASettingWithNoRecordedChangeIsNotJudged(t *testing.T) {
	srv, _, id := settingSwitch(t, nil)
	wantSwitch(t, compare(t, srv, id, nil), experiment.SwitchUnverifiable)

	// The list says the same.
	var list struct {
		Experiments []experiment.Summary `json:"experiments"`
	}
	srv.call(t, "experiments_list", nil).data(t, "experiments_list", &list)
	if r := list.Experiments[0]; r.Verdict != experiment.VerdictSwitchUnconfirmed || r.Judged {
		t.Errorf("list row = %+v, want switch_unconfirmed and not judged", r)
	}
}

func TestARecordedChangeToOtherValuesIsNotTheDeclaredSwitch(t *testing.T) {
	for name, values := range map[string][2]string{
		"another after":  {"1", "9"},
		"another before": {"3", "0.5"},
	} {
		t.Run(name, func(t *testing.T) {
			srv, w, id := settingSwitch(t, nil)
			auditRow(t, w, startedAt, "set", key, values[0], values[1])
			c := compare(t, srv, id, nil)
			// A save of the target to another value is also a confounder, which
			// is reported first; either way there is no verdict.
			if c.Switch.Status != experiment.SwitchMismatch || c.Judged || (c.Verdict != experiment.VerdictSwitchUnconfirmed && c.Verdict != experiment.VerdictConfounded) {
				t.Fatalf("switch check = %+v, verdict %s judged=%v, want a mismatch and no verdict", c.Switch, c.Verdict, c.Judged)
			}
		})
	}
}

// A switch whose audit_id is given is read from that row: a row that left the
// value as it was (the same value saved again) is no switch at all.
func TestAnAuditRowThatChangedNothingIsNoSwitch(t *testing.T) {
	srv, w := newServer(t)
	plantImproved(t, w)
	noop := auditRow(t, w, startedAt, "set", key, "0.5", "0.50")
	e := createUnrecorded(t, srv, createArgs(key, map[string]any{"audit_id": noop}))
	closeAt(t, srv, e.ID, endedAt)
	wantSwitch(t, compare(t, srv, e.ID, nil), experiment.SwitchNotSwitched)
}

// The audit_id row is checked against the declaration, too.
func TestTheAuditIDRowMustBeTheDeclaredSwitch(t *testing.T) {
	srv, w := newServer(t)
	plantImproved(t, w)
	other := auditRow(t, w, startedAt, "set", key, "2", "4")
	e := createUnrecorded(t, srv, createArgs(key, map[string]any{"audit_id": other}))
	closeAt(t, srv, e.ID, endedAt)
	wantSwitch(t, compare(t, srv, e.ID, nil), experiment.SwitchMismatch)
}

// The declared start is when the change took effect: the recorded switch must
// lie within experiment.StartTolerance of it, or one period ran on both values.
func TestTheSwitchMustBeSavedAtTheDeclaredStart(t *testing.T) {
	for _, tt := range []struct {
		name, at string
		want     string
	}{
		{"at the start", "2026-10-01T15:00:00Z", experiment.SwitchConfirmed},
		{"14 minutes before", "2026-10-01T14:46:00Z", experiment.SwitchConfirmed},
		{"14 minutes after", "2026-10-01T15:14:00Z", experiment.SwitchConfirmed},
		{"16 minutes before (the before period ran partly on after)", "2026-10-01T14:44:00Z", experiment.SwitchMismatch},
		{"16 minutes after (no record near the start)", "2026-10-01T15:16:00Z", experiment.SwitchUnverifiable},
		{"a day before the start", "2026-09-30T15:00:00Z", experiment.SwitchMismatch},
	} {
		t.Run(tt.name, func(t *testing.T) {
			srv, w, id := settingSwitch(t, nil)
			auditRow(t, w, tt.at, "set", key, "1", "0.5")
			c := compare(t, srv, id, nil)
			wantSwitch(t, c, tt.want)
		})
	}
}

// Without an audit_id the switch is the newest row of the key that changed the
// value: a later save of the same value does not hide it, a save of another
// value does not count as it.
func TestWithoutAnAuditIDTheNewestChangingRowIsTheSwitch(t *testing.T) {
	srv, w, id := settingSwitch(t, nil)
	auditRow(t, w, startedAt, "set", key, "1", "0.5")
	auditRow(t, w, "2026-10-01T15:05:00Z", "set", key, "0.5", "0.50") // saved again, nothing changed
	c := compare(t, srv, id, nil)
	wantSwitch(t, c, experiment.SwitchConfirmed)
	if c.Confounded {
		t.Errorf("a same-value save confounded: %+v", c.Confounders)
	}
}

// A reset to the default has no value to compare with the declared after; the
// declared before is still checked.
func TestAResetIsCheckedOnItsKnownSide(t *testing.T) {
	srv, w := newServer(t)
	plantImproved(t, w)
	testkit.MustExec(t, w, `INSERT INTO mcp_settings_audit (at, actor, client, action, setting_key, before_value, after_value)
		VALUES (?, 'gui', '', 'reset', ?, '1', NULL)`, testkit.Stored(t, startedAt), key)
	var reset int64
	if err := w.QueryRow(`SELECT id FROM mcp_settings_audit WHERE action = 'reset'`).Scan(&reset); err != nil {
		t.Fatal(err)
	}
	e := createUnrecorded(t, srv, createArgs(key, map[string]any{"audit_id": reset}))
	closeAt(t, srv, e.ID, endedAt)
	wantSwitch(t, compare(t, srv, e.ID, nil), experiment.SwitchConfirmed)
}

func modelExperiment(t *testing.T) (*testServer, *sql.DB, int64) {
	t.Helper()
	srv, w := newServer(t)
	plantImproved(t, w)
	e := createUnrecorded(t, srv, modelExperimentArgs(startedAt))
	closeAt(t, srv, e.ID, endedAt)
	return srv, w, e.ID
}

// A model experiment is confirmed by the decisions: model-a ran before the
// start and model-b after it.
func TestAModelSwitchIsConfirmedByTheDecisions(t *testing.T) {
	srv, w, id := modelExperiment(t)
	decisionRow(t, w, midBefore, "trader", "model-a", "trader-v3")
	decisionRow(t, w, inside, "trader", "model-b", "trader-v3")
	decisionRow(t, w, inside, "scout", "scout-model", "scout-v1") // the scout's own model never changed
	wantSwitch(t, compare(t, srv, id, nil), experiment.SwitchConfirmed)
}

func TestAModelSwitchTheDecisionsDoNotShowIsNotJudged(t *testing.T) {
	for name, tt := range map[string]struct {
		rows   [][3]string // at, decision type, model
		status string
	}{
		"no decision at all":     {nil, experiment.SwitchUnverifiable},
		"no decision before":     {[][3]string{{inside, "trader", "model-b"}}, experiment.SwitchUnverifiable},
		"no decision after":      {[][3]string{{midBefore, "trader", "model-a"}}, experiment.SwitchUnverifiable},
		"the same model on both": {[][3]string{{midBefore, "trader", "model-b"}, {inside, "trader", "model-b"}}, experiment.SwitchNotSwitched},
		"another model after":    {[][3]string{{midBefore, "trader", "model-a"}, {inside, "trader", "model-c"}}, experiment.SwitchMismatch},
		"another model before":   {[][3]string{{midBefore, "trader", "model-c"}, {inside, "trader", "model-b"}}, experiment.SwitchMismatch},
		"after used long before": {[][3]string{{midBefore, "trader", "model-a"}, {"2026-10-01T12:00:00Z", "trader", "model-b"}, {inside, "trader", "model-b"}}, experiment.SwitchMismatch},
		"before used long after": {[][3]string{{midBefore, "trader", "model-a"}, {inside, "trader", "model-a"}, {"2026-10-02T11:00:00Z", "trader", "model-b"}}, experiment.SwitchMismatch},
		"after used near start":  {[][3]string{{midBefore, "trader", "model-a"}, {"2026-10-01T14:50:00Z", "trader", "model-b"}, {inside, "trader", "model-b"}}, experiment.SwitchConfirmed},
		"before used near start": {[][3]string{{midBefore, "trader", "model-a"}, {"2026-10-01T15:10:00Z", "trader", "model-a"}, {inside, "trader", "model-b"}}, experiment.SwitchConfirmed},
	} {
		t.Run(name, func(t *testing.T) {
			srv, w, id := modelExperiment(t)
			for _, r := range tt.rows {
				decisionRow(t, w, r[0], r[1], r[2], "trader-v3")
			}
			c := compare(t, srv, id, nil)
			if c.Switch.Status != tt.status {
				t.Fatalf("switch check = %+v, want %s (confounders %+v)", c.Switch, tt.status, c.Confounders)
			}
			if tt.status != experiment.SwitchConfirmed && (c.Judged || c.Verdict != experiment.VerdictSwitchUnconfirmed) {
				t.Errorf("verdict %s judged=%v, want switch_unconfirmed", c.Verdict, c.Judged)
			}
		})
	}
}

// The prompt version is checked the same way, on question_version.
func TestAPromptSwitchIsConfirmedByTheDecisions(t *testing.T) {
	srv, w := newServer(t)
	plantImproved(t, w)
	args := modelExperimentArgs(startedAt)
	args["changes"] = []map[string]any{{"kind": "prompt", "name": "trader", "before": "trader-v2", "after": "trader-v3"}}
	e := createUnrecorded(t, srv, args)
	closeAt(t, srv, e.ID, endedAt)

	decisionRow(t, w, midBefore, "trader", "model-a", "trader-v2")
	decisionRow(t, w, inside, "trader", "model-a", "trader-v2")
	if c := compare(t, srv, e.ID, nil); c.Switch.Status != experiment.SwitchNotSwitched {
		t.Fatalf("switch check = %+v, want not_switched while only the old version ran", c.Switch)
	}
	decisionRow(t, w, "2026-10-03T10:00:00Z", "trader", "model-a", "trader-v3") // the new one, but the old is still in use
	if c := compare(t, srv, e.ID, nil); c.Switch.Status != experiment.SwitchMismatch {
		t.Fatalf("switch check = %+v, want mismatch while the old version is still in use", c.Switch)
	}
	testkit.MustExec(t, w, `DELETE FROM jev_decisions WHERE timestamp = ?`, testkit.Stored(t, inside))
	wantSwitch(t, compare(t, srv, e.ID, nil), experiment.SwitchConfirmed)
}
