package experimenttests

import (
	"database/sql"
	"net/http"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/experiment"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

// modelSwitchRow plants a row of the Settings screen's record of switching the
// Jev model (jev_model_changes, issue #885); a nil value is "unset".
func modelSwitchRow(t *testing.T, w *sql.DB, at string, before, after any) {
	t.Helper()
	testkit.MustExec(t, w, `INSERT INTO jev_model_changes (at, actor, before_value, after_value) VALUES (?, 'gui', ?, ?)`,
		testkit.Stored(t, at), before, after)
}

func modelExperimentArgs(started string) map[string]any {
	return map[string]any{
		"hypothesis": "モデルを a から b に替えると純損益が増える",
		"changes":    []map[string]any{{"kind": "model", "name": "jev-model", "before": "model-a", "after": "model-b"}},
		"started_at": started,
	}
}

// issue #885: a model switch saved in the experiment's period is a confounder
// even though no Jev decision has run (jev_decisions holds nothing).
func TestRecordedModelSwitchConfoundsBeforeAnyDecisionRan(t *testing.T) {
	srv, w := newServer(t)
	e := create(t, srv, createArgs("risk.paper.max_trade_loss_pct", nil))
	closeAt(t, srv, e.ID, endedAt)

	modelSwitchRow(t, w, beforeTime, "model-x", "model-y") // outside the period
	modelSwitchRow(t, w, afterTime, "model-y", "model-z")
	if c := confoundersOf(t, srv, e.ID); c.Confounded {
		t.Fatalf("a switch outside the period confounded: %+v", c.Confounders)
	}

	modelSwitchRow(t, w, inside, "model-a", "model-b")
	c := confoundersOf(t, srv, e.ID)
	if !c.Confounded || c.Verdict != experiment.VerdictConfounded || len(c.Confounders) != 1 {
		t.Fatalf("confounded=%v verdict=%s confounders=%+v, want one model confounder", c.Confounded, c.Verdict, c.Confounders)
	}
	f := c.Confounders[0]
	if f.Kind != experiment.KindModel || f.Name != experiment.ModelSwitchName || f.At != "2026-10-02T10:00:00Z" ||
		len(f.Values) != 2 || f.Values[0] != "model-a" || f.Values[1] != "model-b" {
		t.Errorf("confounder = %+v, want the JEV_MODEL switch model-a -> model-b at 2026-10-02T10:00:00Z", f)
	}
}

// A switch to or from "unset" (the default model) is shown as such.
func TestRecordedModelSwitchToUnsetIsShownAsUnset(t *testing.T) {
	srv, w := newServer(t)
	e := create(t, srv, createArgs("risk.paper.max_trade_loss_pct", nil))
	closeAt(t, srv, e.ID, endedAt)
	modelSwitchRow(t, w, inside, "model-a", nil)

	c := confoundersOf(t, srv, e.ID)
	if len(c.Confounders) != 1 || c.Confounders[0].Values[0] != "model-a" || c.Confounders[0].Values[1] != experiment.UnsetModel {
		t.Fatalf("confounders = %+v, want model-a -> %s", c.Confounders, experiment.UnsetModel)
	}
}

// A model experiment's own switch (to the model it names) is not a confounder,
// another recorded switch is. A switch saved while no decision ran is enough.
func TestModelExperimentsOwnRecordedSwitchDoesNotConfound(t *testing.T) {
	srv, w := newServer(t)
	modelSwitchRow(t, w, "2026-10-01T10:00:00Z", "model-a", "model-b") // saved before the declared start
	e := create(t, srv, modelExperimentArgs(startedAt))
	closeAt(t, srv, e.ID, endedAt)
	if c := confoundersOf(t, srv, e.ID); c.Confounded {
		t.Fatalf("the experiment's own switch confounded: %+v", c.Confounders)
	}

	modelSwitchRow(t, w, inside, "model-b", "model-c")
	if c := confoundersOf(t, srv, e.ID); !c.Confounded || c.Confounders[0].Name != experiment.ModelSwitchName {
		t.Fatalf("a further switch did not confound: %+v", c.Confounders)
	}
}

// The declared start of a model experiment may not precede the saved switch to
// its After model; with no record (before the record began) it is believed.
func TestModelExperimentStartIsCheckedAgainstTheRecord(t *testing.T) {
	srv, w := newServer(t)
	if e := create(t, srv, modelExperimentArgs(startedAt)); e.ID == 0 {
		t.Fatal("with no record the declared start was refused")
	}

	modelSwitchRow(t, w, "2026-10-02T10:00:00Z", "model-a", "model-b")
	srv.call(t, "experiment_create", modelExperimentArgs(startedAt)).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidArgument)
	if n := count(t, w, `SELECT COUNT(*) FROM tuning_experiments`); n != 1 {
		t.Errorf("experiments = %d after the refused start, want 1", n)
	}

	if e := create(t, srv, modelExperimentArgs("2026-10-02T10:00:00Z")); e.ID == 0 {
		t.Fatal("a start at the switch time was refused")
	}
	// A switch to another model does not bear on this experiment's start.
	modelSwitchRow(t, w, "2026-10-02T12:00:00Z", "model-b", "model-c")
	if e := create(t, srv, modelExperimentArgs("2026-10-02T11:00:00Z")); e.ID == 0 {
		t.Fatal("a switch to another model refused the start")
	}
}
