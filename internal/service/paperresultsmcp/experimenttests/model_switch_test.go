package experimenttests

import (
	"database/sql"
	"net/http"
	"strings"
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
	if f.Period != experiment.PeriodAfter {
		t.Errorf("period = %q, want after (saved inside the experiment's period)", f.Period)
	}
}

// issue #886: a switch saved in the before period confounds too, with the
// period before; a recorded switch has a time, so it is never a boundary one.
func TestRecordedModelSwitchInTheBeforePeriodConfounds(t *testing.T) {
	srv, w, id := settingExperiment(t)
	modelSwitchRow(t, w, midBefore, "model-a", "model-b")
	f := wantConfounded(t, confoundersOf(t, srv, id), experiment.KindModel, experiment.ModelSwitchName, experiment.PeriodBefore)
	if f.At != "2026-10-01T10:00:00Z" || len(f.Values) != 2 || f.Values[0] != "model-a" || f.Values[1] != "model-b" {
		t.Errorf("confounder = %+v, want model-a -> model-b at 2026-10-01T10:00:00Z", f)
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
	if c := confoundersOf(t, srv, e.ID); !c.Confounded || c.Confounders[0].Name != experiment.ModelSwitchName || c.Confounders[0].Period != experiment.PeriodAfter {
		t.Fatalf("a further switch did not confound: %+v", c.Confounders)
	}
}

// The own switch is left out in the before period as well, because the change
// is saved shortly before the start; any other switch there confounds.
func TestModelExperimentsOtherSwitchInTheBeforePeriodConfounds(t *testing.T) {
	srv, w := newServer(t)
	modelSwitchRow(t, w, "2026-10-01T10:00:00Z", "model-a", "model-z")
	modelSwitchRow(t, w, "2026-10-01T11:00:00Z", "model-z", "model-b") // own switch, before the start
	e := create(t, srv, modelExperimentArgs("2026-10-01T12:00:00Z"))
	closeAt(t, srv, e.ID, endedAt)
	c := confoundersOf(t, srv, e.ID)
	if len(c.Confounders) != 1 || c.Confounders[0].Period != experiment.PeriodBefore || c.Confounders[0].Values[1] != "model-z" {
		t.Fatalf("confounders = %+v, want only the switch to model-z in the before period", c.Confounders)
	}
}

// started_at of a model experiment is checked against the model in effect at
// that time, read from the record: the latest switch at or before it, or the
// before value of the oldest switch when there is none.
func TestModelExperimentStartIsCheckedAgainstTheRecord(t *testing.T) {
	srv, w := newServer(t)
	if e := create(t, srv, modelExperimentArgs(startedAt)); e.ID == 0 {
		t.Fatal("with no record the declared start was refused")
	}

	modelSwitchRow(t, w, "2026-10-02T10:00:00Z", "model-a", "model-b")
	r := srv.call(t, "experiment_create", modelExperimentArgs(startedAt))
	r.wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidArgument)
	// The saved time is shown in JST (the Settings screen takes JST).
	if !strings.Contains(string(r.body), "2026-10-02 19:00:00（JST）") || !strings.Contains(string(r.body), "model-a") {
		t.Errorf("message = %s, want the model in use (model-a) and the switch time in JST", r.body)
	}
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

// model-b was in use, then model-c, then model-b again (t1, t2, t3). A start
// while b was in use is right wherever the latest switch to b falls; a start
// while c was in use is not, and neither is one before b first came in.
func TestModelExperimentStartWhenTheAfterModelCameBackLater(t *testing.T) {
	srv, w := newServer(t)
	modelSwitchRow(t, w, "2026-09-30T10:00:00Z", "model-a", "model-b") // t1
	modelSwitchRow(t, w, "2026-10-01T10:00:00Z", "model-b", "model-c") // t2
	modelSwitchRow(t, w, "2026-10-02T10:00:00Z", "model-c", "model-b") // t3

	for _, started := range []string{"2026-09-30T10:00:00Z", "2026-10-01T09:59:59Z", "2026-10-02T10:00:00Z", "2026-10-02T11:00:00Z"} {
		if e := create(t, srv, modelExperimentArgs(started)); e.ID == 0 {
			t.Errorf("a start at %s, while model-b was in use, was refused", started)
		}
	}
	for _, started := range []string{"2026-09-30T09:59:59Z", "2026-10-01T10:00:00Z", "2026-10-02T09:59:59Z"} {
		srv.call(t, "experiment_create", modelExperimentArgs(started)).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidArgument)
	}
}

// The model in use before the record began is the oldest switch's before value:
// an experiment on that model may start before the record.
func TestModelExperimentStartBeforeTheRecordWithTheModelAlreadyInUse(t *testing.T) {
	srv, w := newServer(t)
	modelSwitchRow(t, w, "2026-10-02T10:00:00Z", "model-b", "model-c")
	if e := create(t, srv, modelExperimentArgs("2026-10-01T00:00:00Z")); e.ID == 0 {
		t.Fatal("model-b was in use before the record began, but the start was refused")
	}
}

// An unset model (the default one) is not known by name, so it cannot show the
// After model was not in use.
func TestModelExperimentStartWithAnUnsetModelInEffectIsBelieved(t *testing.T) {
	srv, w := newServer(t)
	modelSwitchRow(t, w, "2026-10-02T10:00:00Z", nil, "model-b")
	if e := create(t, srv, modelExperimentArgs("2026-10-01T00:00:00Z")); e.ID == 0 {
		t.Fatal("the default model was in use at the start, but the start was refused")
	}
	modelSwitchRow(t, w, "2026-10-03T10:00:00Z", "model-b", nil)
	modelSwitchRow(t, w, "2026-10-04T10:00:00Z", nil, "model-b")
	if e := create(t, srv, modelExperimentArgs("2026-10-03T11:00:00Z")); e.ID == 0 {
		t.Fatal("the model was unset at the start, but the start was refused")
	}
}
