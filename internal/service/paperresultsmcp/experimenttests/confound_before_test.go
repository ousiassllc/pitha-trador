package experimenttests

import (
	"database/sql"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/experiment"
)

// 交絡の検出（前の期間, issue #886）: the before period is the same length just
// before the start. A change of something other than the experiment's variable
// there makes the two periods differ in more than the variable, so the
// experiment is confounded and gives no verdict, exactly as for the after period.

// wantConfounded asserts a confounded comparison with no verdict whose only
// confounder is of kind, name and period.
func wantConfounded(t *testing.T, c experiment.Comparison, kind, name, period string) experiment.Confounder {
	t.Helper()
	if !c.Confounded || c.Verdict != experiment.VerdictConfounded || c.Judged {
		t.Fatalf("confounded=%v verdict=%s judged=%v, want a confounded experiment with no verdict", c.Confounded, c.Verdict, c.Judged)
	}
	if len(c.Confounders) != 1 || c.Confounders[0].Kind != kind || c.Confounders[0].Name != name || c.Confounders[0].Period != period {
		t.Fatalf("confounders = %+v, want one %s %s in the %s period", c.Confounders, kind, name, period)
	}
	return c.Confounders[0]
}

// settingExperiment is a closed risk.paper.max_trade_loss_pct experiment over
// planted trades that would be judged improved if nothing else changed.
func settingExperiment(t *testing.T) (*testServer, *sql.DB, int64) {
	t.Helper()
	srv, w := newServer(t)
	plantImproved(t, w)
	e := create(t, srv, createArgs("risk.paper.max_trade_loss_pct", nil))
	closeAt(t, srv, e.ID, endedAt)
	if c := confoundersOf(t, srv, e.ID); c.Confounded || c.Verdict != experiment.VerdictImproved {
		t.Fatalf("nothing changed yet: confounded=%v verdict=%s", c.Confounded, c.Verdict)
	}
	return srv, w, e.ID
}

func TestAnotherSettingChangingInTheBeforePeriodConfounds(t *testing.T) {
	srv, w, id := settingExperiment(t)
	auditID := auditRow(t, w, midBefore, "set", "risk.max_daily_trades", "5", "3")
	f := wantConfounded(t, confoundersOf(t, srv, id), experiment.KindSetting, "risk.max_daily_trades", experiment.PeriodBefore)
	if f.AuditID == nil || *f.AuditID != auditID {
		t.Errorf("audit id = %v, want %d", f.AuditID, auditID)
	}
}

func TestJevModelChangingInTheBeforePeriodConfounds(t *testing.T) {
	srv, w, id := settingExperiment(t)
	decisionRow(t, w, midBefore, "trader", "model-a", "trader-v3")
	decisionRow(t, w, "2026-10-01T12:00:00Z", "trader", "model-b", "trader-v3")
	f := wantConfounded(t, confoundersOf(t, srv, id), experiment.KindModel, "trader", experiment.PeriodBefore)
	if len(f.Values) != 2 {
		t.Errorf("values = %v, want the two models", f.Values)
	}
}

func TestJevPromptChangingInTheBeforePeriodConfounds(t *testing.T) {
	srv, w, id := settingExperiment(t)
	decisionRow(t, w, midBefore, "scout", "scout-model", "scout-v1")
	decisionRow(t, w, "2026-10-01T12:00:00Z", "scout", "scout-model", "scout-v2")
	f := wantConfounded(t, confoundersOf(t, srv, id), experiment.KindPrompt, "scout", experiment.PeriodBefore)
	if len(f.Values) != 2 {
		t.Errorf("values = %v, want the two prompt versions", f.Values)
	}
}

// Both periods are checked and each confounder names its own.
func TestConfoundersNameTheirPeriod(t *testing.T) {
	srv, w, id := settingExperiment(t)
	auditRow(t, w, midBefore, "set", "risk.max_daily_trades", "5", "3")
	auditRow(t, w, inside, "set", "risk.max_daily_trades", "3", "4")
	c := confoundersOf(t, srv, id)
	if len(c.Confounders) != 2 || c.Confounders[0].Period != experiment.PeriodBefore || c.Confounders[1].Period != experiment.PeriodAfter {
		t.Fatalf("confounders = %+v, want the before one then the after one", c.Confounders)
	}
}

// The experiment's own change next to its start is not a confounder, from
// whichever side of the start it falls in: its audit row, a save of the target
// to the new value, and the model / prompt switching within a period. Moving
// the target to another value in the before period is one.
func TestTheExperimentsOwnChangeInTheBeforePeriodIsNotAConfounder(t *testing.T) {
	srv, w := newServer(t)
	own := auditRow(t, w, "2026-10-01T14:00:00Z", "set", "risk.paper.max_trade_loss_pct", "1", "0.5") // an hour before the start
	e := create(t, srv, createArgs("risk.paper.max_trade_loss_pct", map[string]any{"audit_id": own}))
	closeAt(t, srv, e.ID, endedAt)
	auditRow(t, w, "2026-10-01T14:30:00Z", "set", "risk.paper.max_trade_loss_pct", "0.5", "0.50")
	if c := confoundersOf(t, srv, e.ID); c.Confounded {
		t.Fatalf("the experiment's own change in the before period confounded: %+v", c.Confounders)
	}
	other := auditRow(t, w, midBefore, "set", "risk.paper.max_trade_loss_pct", "1", "2")
	wantConfounded(t, confoundersOf(t, srv, e.ID), experiment.KindSetting, "risk.paper.max_trade_loss_pct", experiment.PeriodBefore)
	if f := confoundersOf(t, srv, e.ID).Confounders[0]; f.AuditID == nil || *f.AuditID != other {
		t.Errorf("confounder = %+v, want audit id %d", f, other)
	}

	// Without an audit id, a save of the target to the new value is its own change.
	srv2, w2 := newServer(t)
	e2 := create(t, srv2, createArgs("risk.paper.max_trade_loss_pct", nil))
	closeAt(t, srv2, e2.ID, endedAt)
	auditRow(t, w2, "2026-10-01T14:00:00Z", "set", "risk.paper.max_trade_loss_pct", "1", "0.5")
	if c := confoundersOf(t, srv2, e2.ID); c.Confounded {
		t.Fatalf("the target saved to the new value confounded: %+v", c.Confounders)
	}
}

// A model (prompt) experiment's own switch may fall in the before period; the
// other axis changing there is still a confounder.
func TestAModelOrPromptExperimentsOwnSwitchInTheBeforePeriodIsNotAConfounder(t *testing.T) {
	cases := []struct {
		kind, otherKind string
		own, other      [3]string // time, model, prompt
	}{
		{experiment.KindModel, experiment.KindPrompt, [3]string{"2026-10-01T14:00:00Z", "m-new", "p-1"}, [3]string{"2026-10-01T13:00:00Z", "m-old", "p-2"}},
		{experiment.KindPrompt, experiment.KindModel, [3]string{"2026-10-01T14:00:00Z", "m-1", "p-new"}, [3]string{"2026-10-01T13:00:00Z", "m-2", "p-old"}},
	}
	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			srv, w := newServer(t)
			e := create(t, srv, map[string]any{
				"hypothesis": "Jev の " + tc.kind + " を替える",
				"changes":    []map[string]any{{"kind": tc.kind, "name": "trader", "before": "old", "after": "new"}},
				"started_at": startedAt,
			})
			closeAt(t, srv, e.ID, endedAt)
			model, prompt := "m-old", "p-old"
			if tc.kind == experiment.KindModel {
				prompt = "p-1"
			} else {
				model = "m-1"
			}
			decisionRow(t, w, midBefore, "trader", model, prompt)
			decisionRow(t, w, tc.own[0], "trader", tc.own[1], tc.own[2]) // the switch, an hour before the start
			if c := confoundersOf(t, srv, e.ID); c.Confounded {
				t.Fatalf("its own switch in the before period confounded: %+v", c.Confounders)
			}
			decisionRow(t, w, tc.other[0], "trader", tc.other[1], tc.other[2]) // the other axis changes too
			wantConfounded(t, confoundersOf(t, srv, e.ID), tc.otherKind, "trader", experiment.PeriodBefore)
		})
	}
}
