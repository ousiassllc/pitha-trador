package experimenttests

import (
	"database/sql"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/experiment"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

// roleChangeRow plants a row of ai_role_history (issue #1080): a 'saved' row is
// the Settings screen's change of a role's provider/model, a 'started' row is
// the app's start record.
func roleChangeRow(t *testing.T, w *sql.DB, at, kind, role, beforeProvider, beforeModel, provider, model string) {
	t.Helper()
	if kind == "saved" {
		testkit.MustExec(t, w, `INSERT INTO ai_role_history (at, kind, actor, role, before_provider, before_model, provider, model) VALUES (?, 'saved', 'gui', ?, ?, ?, ?, ?)`,
			testkit.Stored(t, at), role, beforeProvider, beforeModel, provider, model)
		return
	}
	testkit.MustExec(t, w, `INSERT INTO ai_role_history (at, kind, actor, role, provider, model) VALUES (?, 'started', 'app', ?, ?, ?)`,
		testkit.Stored(t, at), role, provider, model)
}

// Issue #1080 (tied to #878/#885): switching the live Jev role's provider or model on the
// Settings screen confounds a tuning experiment like a JEV_MODEL switch does. Only the live
// role counts, and a start record is not a switch.
func TestLiveJevRoleSwitchConfoundsAnExperiment(t *testing.T) {
	srv, w := newServer(t)
	e := create(t, srv, createArgs("risk.paper.max_trade_loss_pct", nil))
	closeAt(t, srv, e.ID, endedAt)

	roleChangeRow(t, w, inside, "saved", "sol", "custom", "", "openrouter", "vendor/sol") // not the live role
	roleChangeRow(t, w, inside, "started", "jev", "", "", "openrouter", "vendor/jev")     // a start record
	roleChangeRow(t, w, beforeTime, "saved", "jev", "custom", "", "openrouter", "x/y")    // outside the period
	if c := confoundersOf(t, srv, e.ID); c.Confounded {
		t.Fatalf("a non-live role, a start record or a switch outside the period confounded: %+v", c.Confounders)
	}

	roleChangeRow(t, w, inside, "saved", "jev", "custom", "", "openrouter", "vendor/jev")
	c := confoundersOf(t, srv, e.ID)
	if !c.Confounded || c.Verdict != experiment.VerdictConfounded || len(c.Confounders) != 1 {
		t.Fatalf("confounded=%v verdict=%s confounders=%+v, want one", c.Confounded, c.Verdict, c.Confounders)
	}
	f := c.Confounders[0]
	if f.Kind != experiment.KindModel || f.Name != experiment.RoleSwitchName || f.Period != experiment.PeriodAfter ||
		len(f.Values) != 2 || f.Values[0] != "custom / "+experiment.UnsetModel || f.Values[1] != "openrouter / vendor/jev" {
		t.Errorf("confounder = %+v", f)
	}
}
