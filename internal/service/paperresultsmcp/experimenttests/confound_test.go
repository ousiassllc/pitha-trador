package experimenttests

import (
	"database/sql"
	"net/http"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/experiment"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

// Rows planted straight into the database: the changes an experiment must
// notice are made by the application (the Settings screen, a model switch)
// and by the settings tools, all of which end up as these rows.

var nextAudit, nextDecision = 0, 100

func auditRow(t *testing.T, w *sql.DB, at, action, key, before, after string) int64 {
	t.Helper()
	nextAudit++
	testkit.MustExec(t, w, `INSERT INTO mcp_settings_audit (id, at, actor, client, action, setting_key, before_value, after_value)
		VALUES (?, ?, 'gui', '', ?, ?, ?, ?)`, 1000+nextAudit, testkit.Stored(t, at), action, key, before, after)
	return int64(1000 + nextAudit)
}

func decisionRow(t *testing.T, w *sql.DB, at, decisionType, model, prompt string) {
	t.Helper()
	nextDecision++
	testkit.MustExec(t, w, `INSERT INTO jev_decisions (id, instrument_id, symbol, timestamp, decision_type, state_hash,
		state_json, question_version, response_json, direction, confidence, latency_ms, model_id)
		VALUES (?, 1, '7203', ?, ?, 'h', '{}', ?, '{}', 'LONG', 0.5, 5, ?)`, nextDecision, testkit.Stored(t, at), decisionType, prompt, model)
}

const (
	inside     = "2026-10-02T10:00:00Z" // within [startedAt, endedAt)
	beforeTime = "2026-10-01T10:00:00Z" // before startedAt
	afterTime  = "2026-10-04T10:00:00Z" // after endedAt
)

// confounders runs experiment_compare with a minimum small enough to judge and
// returns the comparison.
func confoundersOf(t *testing.T, srv *testServer, id int64) experiment.Comparison {
	t.Helper()
	return compare(t, srv, id, map[string]any{"min_trades": 1})
}

// 交絡の検出: while the experiment ran, a whitelisted setting other than the
// target changed -> confounded, no verdict, the figures still there.
func TestAnotherSettingChangingDuringTheExperimentConfounds(t *testing.T) {
	srv, w := newServer(t)
	e := create(t, srv, createArgs("risk.paper.max_trade_loss_pct", nil))
	closeAt(t, srv, e.ID, endedAt)
	if c := confoundersOf(t, srv, e.ID); c.Confounded || c.Verdict != experiment.VerdictImproved {
		t.Fatalf("nothing changed yet: confounded=%v verdict=%s", c.Confounded, c.Verdict)
	}

	// Outside the experiment, or not a tuning variable: not a confounder.
	auditRow(t, w, beforeTime, "set", "risk.max_daily_trades", "5", "3")
	auditRow(t, w, afterTime, "set", "risk.max_daily_trades", "3", "4")
	auditRow(t, w, inside, "write_disable", "mcp.settings_write_enabled", "1", "0")
	auditRow(t, w, inside, "set", "not.on.the.whitelist", "1", "2")
	if c := confoundersOf(t, srv, e.ID); c.Confounded {
		t.Fatalf("changes outside the window or off the whitelist confounded: %+v", c.Confounders)
	}

	id := auditRow(t, w, inside, "set", "risk.max_daily_trades", "3", "4")
	c := confoundersOf(t, srv, e.ID)
	if !c.Confounded || c.Verdict != experiment.VerdictConfounded || c.Judged {
		t.Fatalf("confounded=%v verdict=%s judged=%v, want a confounded experiment with no verdict", c.Confounded, c.Verdict, c.Judged)
	}
	if len(c.Confounders) != 1 || c.Confounders[0].Kind != experiment.KindSetting || c.Confounders[0].Name != "risk.max_daily_trades" ||
		c.Confounders[0].AuditID == nil || *c.Confounders[0].AuditID != id {
		t.Errorf("confounders = %+v", c.Confounders)
	}
	if c.After.Performance.ClosedTradeCount != 3 {
		t.Errorf("the figures must still be returned: %+v", c.After.Performance)
	}

	var list struct {
		Experiments []experiment.Summary `json:"experiments"`
	}
	srv.call(t, "experiments_list", map[string]any{"min_trades": 1}).data(t, "experiments_list", &list)
	if r := list.Experiments[0]; !r.Confounded || r.Judged || r.Verdict != experiment.VerdictConfounded {
		t.Errorf("list row = %+v, want confounded and not judged", r)
	}
}

// The experiment's own change is not a confounder (its audit row, or a later
// save of the target to the same value), but moving the target again is.
func TestTheTargetSettingItselfOnlyConfoundsWhenItMovesAgain(t *testing.T) {
	srv, w := newServer(t)
	own := auditRow(t, w, startedAt, "set", "risk.paper.max_trade_loss_pct", "1", "0.5")
	e := create(t, srv, createArgs("risk.paper.max_trade_loss_pct", map[string]any{"audit_id": own}))
	closeAt(t, srv, e.ID, endedAt)
	if c := confoundersOf(t, srv, e.ID); c.Confounded {
		t.Fatalf("the experiment's own change confounded: %+v", c.Confounders)
	}
	if e.Variable.AuditID == nil || *e.Variable.AuditID != own {
		t.Errorf("stored audit id = %v, want %d", e.Variable.AuditID, own)
	}

	auditRow(t, w, inside, "set", "risk.paper.max_trade_loss_pct", "0.5", "0.50") // saved again, same value
	if c := confoundersOf(t, srv, e.ID); c.Confounded {
		t.Fatalf("saving the target to the same value confounded: %+v", c.Confounders)
	}
	auditRow(t, w, inside, "set", "risk.paper.max_trade_loss_pct", "0.5", "2")
	if c := confoundersOf(t, srv, e.ID); !c.Confounded {
		t.Fatal("moving the target to another value did not confound")
	}
}

// An audit id must belong to a set / reset of the experiment's own key.
func TestAuditIDMustBeTheTargetSettingsChange(t *testing.T) {
	srv, w := newServer(t)
	other := auditRow(t, w, startedAt, "set", "risk.max_daily_trades", "5", "3")
	srv.call(t, "experiment_create", createArgs("risk.paper.max_trade_loss_pct", map[string]any{"audit_id": other})).
		wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidArgument)
	sw := auditRow(t, w, startedAt, "write_enable", "mcp.settings_write_enabled", "0", "1")
	srv.call(t, "experiment_create", createArgs("risk.paper.max_trade_loss_pct", map[string]any{"audit_id": sw})).
		wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidArgument)
	if n := count(t, w, `SELECT COUNT(*) FROM tuning_experiments`); n != 0 {
		t.Fatalf("%d experiments stored, want none", n)
	}
}

// 交絡の検出 (model / prompt): a setting experiment sees a Jev model or prompt
// version change during the experiment as a confounder.
func TestJevModelOrPromptChangingDuringASettingExperimentConfounds(t *testing.T) {
	srv, w := newServer(t)
	e := create(t, srv, createArgs("risk.paper.max_trade_loss_pct", nil))
	closeAt(t, srv, e.ID, endedAt)

	decisionRow(t, w, beforeTime, "trader", "model-old", "trader-v2") // before the start: fine
	decisionRow(t, w, inside, "trader", "model-a", "trader-v3")
	decisionRow(t, w, "2026-10-03T10:00:00Z", "trader", "model-a", "trader-v3")
	decisionRow(t, w, inside, "scout", "scout-model", "scout-v1") // another decision type has its own model
	if c := confoundersOf(t, srv, e.ID); c.Confounded {
		t.Fatalf("a steady model and prompt confounded: %+v", c.Confounders)
	}

	decisionRow(t, w, "2026-10-03T11:00:00Z", "trader", "model-b", "trader-v3")
	c := confoundersOf(t, srv, e.ID)
	if !c.Confounded || len(c.Confounders) != 1 || c.Confounders[0].Kind != experiment.KindModel || c.Confounders[0].Name != "trader" ||
		len(c.Confounders[0].Values) != 2 || c.Verdict != experiment.VerdictConfounded {
		t.Fatalf("model change: %+v verdict %s", c.Confounders, c.Verdict)
	}

	decisionRow(t, w, "2026-10-03T12:00:00Z", "trader", "model-a", "trader-v4")
	c = confoundersOf(t, srv, e.ID)
	kinds := map[string]bool{}
	for _, f := range c.Confounders {
		kinds[f.Kind] = true
	}
	if !kinds[experiment.KindModel] || !kinds[experiment.KindPrompt] || len(c.Confounders) != 2 {
		t.Errorf("model and prompt change: %+v", c.Confounders)
	}
}

// For a model (or prompt) experiment that variable may show the old and the
// new value (the switch happens inside the period), and nothing else may
// change.
func TestModelExperimentAllowsOnlyItsOwnSwitch(t *testing.T) {
	srv, w := newServer(t)
	e := create(t, srv, map[string]any{
		"hypothesis": "モデルを a から b に替えると純損益が増える",
		"changes":    []map[string]any{{"kind": "model", "name": "jev-model", "before": "model-a", "after": "model-b"}},
		"started_at": startedAt,
	})
	closeAt(t, srv, e.ID, endedAt)

	decisionRow(t, w, inside, "trader", "model-a", "trader-v3") // still the old model right after the start
	decisionRow(t, w, "2026-10-03T10:00:00Z", "trader", "model-b", "trader-v3")
	if c := confoundersOf(t, srv, e.ID); c.Confounded {
		t.Fatalf("the model's own switch confounded: %+v", c.Confounders)
	}

	decisionRow(t, w, "2026-10-03T11:00:00Z", "trader", "model-c", "trader-v3") // a third model
	if c := confoundersOf(t, srv, e.ID); !c.Confounded || c.Confounders[0].Kind != experiment.KindModel {
		t.Fatalf("a third model did not confound: %+v", c.Confounders)
	}

	// A settings change confounds a model experiment too.
	srv2, w2 := newServer(t)
	e2 := create(t, srv2, map[string]any{
		"hypothesis": "プロンプトを v3 から v4 に",
		"changes":    []map[string]any{{"kind": "prompt", "name": "trader", "before": "trader-v3", "after": "trader-v4"}},
		"started_at": startedAt,
	})
	closeAt(t, srv2, e2.ID, endedAt)
	auditRow(t, w2, inside, "set", "risk.max_daily_trades", "5", "3")
	if c := confoundersOf(t, srv2, e2.ID); !c.Confounded || c.Confounders[0].Kind != experiment.KindSetting {
		t.Fatalf("a settings change did not confound a prompt experiment: %+v", c.Confounders)
	}
}

// A running experiment is checked up to now; changes after it ended are not
// part of it.
func TestRunningExperimentIsCheckedUpToNow(t *testing.T) {
	srv, w := newServer(t)
	e := create(t, srv, createArgs("risk.paper.max_trade_loss_pct", nil))
	auditRow(t, w, "2026-10-17T00:00:00Z", "set", "risk.max_daily_trades", "5", "3") // before the fixed now (10-18)
	if c := confoundersOf(t, srv, e.ID); !c.Confounded {
		t.Fatal("a change while the experiment runs did not confound")
	}
	if c := confoundersOf(t, srv, e.ID); c.Experiment.Status != experiment.StatusRunning || c.Experiment.EndedAt != nil {
		t.Errorf("experiment = %+v", c.Experiment)
	}
}
