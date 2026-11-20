package experimentlabeltests

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/experiment"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/experiment/unrecorded"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

type listData struct {
	Experiments []experiment.Summary `json:"experiments"`
	Unrecorded  []unrecorded.Change  `json:"unrecorded_changes"`
}

func listOf(t *testing.T, srv *testServer) (listData, string) {
	t.Helper()
	r := srv.call(t, "experiments_list", nil)
	var d listData
	r.data(t, "experiments_list", &d)
	return d, string(r.body)
}

func versionedDecision(t *testing.T, w *sql.DB, at, model, prompt string) {
	t.Helper()
	nextDecision++
	testkit.MustExec(t, w, `INSERT INTO jev_decisions (id, instrument_id, symbol, timestamp, decision_type, state_hash,
		state_json, question_version, response_json, direction, confidence, latency_ms, model_id)
		VALUES (?, 1, '7203', ?, 'trader', 'h', '{}', ?, '{}', 'NONE', 0.5, 5, ?)`, nextDecision, testkit.Stored(t, at), prompt, model)
}

func find(changes []unrecorded.Change, kind, name string) *unrecorded.Change {
	for i := range changes {
		if changes[i].Kind == kind && changes[i].Name == name {
			return &changes[i]
		}
	}
	return nil
}

// A setting change no experiment records is listed; one that an experiment
// covers (its audit_id, or the same key started within the switch tolerance) is
// not. Changes that are not changes (a same-value save, the write switch, a key
// off the whitelist) and changes older than 14 days are not listed either.
func TestUnrecordedSettingChangesAreListedAndCoveredOnesAreNot(t *testing.T) {
	srv, w := newServer(t)
	uncovered := auditRow(t, w, "2026-10-15T01:00:00Z", "set", "risk.max_daily_trades", "3", "4")
	coveredByID := auditRow(t, w, "2026-10-14T01:00:00Z", "set", key, "1", "0.5")
	srv.call(t, "experiment_create", map[string]any{
		"hypothesis": "h", "changes": []map[string]any{{"kind": "setting", "name": key, "before": 1, "after": 0.5}},
		"started_at": "2026-10-14T01:00:00Z", "audit_id": coveredByID,
	}).data(t, "experiment_create", &experiment.Experiment{})
	auditRow(t, w, "2026-10-14T01:10:00Z", "set", key, "0.5", "0.6")                   // covered by the same key within the switch tolerance of that start
	auditRow(t, w, "2026-10-15T02:00:00Z", "set", "risk.max_daily_trades", "4", "4.0") // same value again
	auditRow(t, w, "2026-10-15T03:00:00Z", "write_disable", "mcp.settings_write_enabled", "1", "0")
	auditRow(t, w, "2026-10-15T04:00:00Z", "set", "not.on.the.whitelist", "1", "2")
	auditRow(t, w, "2026-09-20T01:00:00Z", "set", "risk.max_daily_trades", "2", "3") // older than 14 days

	d, _ := listOf(t, srv)
	if len(d.Unrecorded) != 1 {
		t.Fatalf("unrecorded_changes = %+v, want just the uncovered setting change", d.Unrecorded)
	}
	c := d.Unrecorded[0]
	if c.Kind != "setting" || c.Name != "risk.max_daily_trades" || c.AuditID == nil || *c.AuditID != uncovered || c.ChangedAt != "2026-10-15T01:00:00Z" ||
		c.Before == nil || *c.Before != "3" || c.After == nil || *c.After != "4" {
		t.Errorf("change = %+v", c)
	}
	// The existing members are still there.
	if len(d.Experiments) != 1 {
		t.Errorf("experiments = %d, want 1", len(d.Experiments))
	}
}

// Model switches (jev_model_changes) and prompt versions (jev_decisions) with
// no experiment are listed, newest first; a model experiment to the new model covers its switch.
func TestUnrecordedModelAndPromptChanges(t *testing.T) {
	srv, w := newServer(t)
	testkit.MustExec(t, w, `INSERT INTO jev_model_changes (at, actor, before_value, after_value) VALUES (?, 'gui', 'a', 'b')`, testkit.Stored(t, "2026-10-16T01:00:00Z"))
	testkit.MustExec(t, w, `INSERT INTO jev_model_changes (at, actor, before_value, after_value) VALUES (?, 'gui', 'b', 'c')`, testkit.Stored(t, "2026-10-10T01:00:00Z"))
	srv.call(t, "experiment_create", map[string]any{
		"hypothesis": "h", "changes": []map[string]any{{"kind": "model", "name": "trader", "before": "b", "after": "c"}}, "started_at": "2026-10-10T01:00:00Z",
	}).data(t, "experiment_create", &experiment.Experiment{})
	versionedDecision(t, w, "2026-10-05T01:00:00Z", "m", "trader-v4")
	versionedDecision(t, w, "2026-10-12T01:00:00Z", "m", "trader-v5")

	d, _ := listOf(t, srv)
	if len(d.Unrecorded) != 2 {
		t.Fatalf("unrecorded_changes = %+v, want the model switch to b and the prompt change", d.Unrecorded)
	}
	if d.Unrecorded[0].Kind != "model" || d.Unrecorded[0].Name != experiment.ModelSwitchName || d.Unrecorded[0].After == nil || *d.Unrecorded[0].After != "b" {
		t.Errorf("newest = %+v, want the model switch (2026-10-16)", d.Unrecorded[0])
	}
	p := find(d.Unrecorded, "prompt", "trader")
	if p == nil || p.Before == nil || *p.Before != "trader-v4" || p.After == nil || *p.After != "trader-v5" || p.ChangedAt != "2026-10-12T01:00:00Z" {
		t.Errorf("prompt change = %+v", p)
	}
}

// At most 20 changes, newest first; the call never writes.
func TestUnrecordedChangesAreBoundedAndNewestFirst(t *testing.T) {
	srv, w := newServer(t)
	for i := range 25 {
		auditRow(t, w, fmt.Sprintf("2026-10-%02dT01:%02d:00Z", 10+i/10*4, i), "set", "risk.max_daily_trades", fmt.Sprint(i), fmt.Sprint(i+1))
	}
	before := count(t, w, `SELECT COUNT(*) FROM mcp_settings_audit`)
	d, body := listOf(t, srv)
	if len(d.Unrecorded) != unrecorded.MaxChanges {
		t.Fatalf("unrecorded_changes = %d, want %d", len(d.Unrecorded), unrecorded.MaxChanges)
	}
	for i := 1; i < len(d.Unrecorded); i++ {
		if d.Unrecorded[i-1].ChangedAt < d.Unrecorded[i].ChangedAt {
			t.Fatalf("not newest first at %d: %s < %s", i, d.Unrecorded[i-1].ChangedAt, d.Unrecorded[i].ChangedAt)
		}
	}
	if count(t, w, `SELECT COUNT(*) FROM mcp_settings_audit`) != before || count(t, w, `SELECT COUNT(*) FROM tuning_experiments`) != 0 {
		t.Error("experiments_list wrote something")
	}
	for _, secret := range append(testkit.Sentinels(), testAPIKey) {
		if strings.Contains(body, secret) {
			t.Errorf("the response contains %q", secret)
		}
	}
}

// The label-based member is free of secrets too.
func TestLabelBasedOutputHasNoSecrets(t *testing.T) {
	srv, w := newServer(t)
	plantLabels(t, w, 5, -2, 4)
	e := createExperiment(t, srv, plantedSwitch(t, w))
	body := string(srv.call(t, "experiment_compare", map[string]any{"experiment_id": e.ID}).body)
	for _, secret := range append(testkit.Sentinels(), testAPIKey) {
		if strings.Contains(body, secret) {
			t.Errorf("the response contains %q", secret)
		}
	}
}
