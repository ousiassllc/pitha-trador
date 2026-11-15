package experimenttests

import (
	"database/sql"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/experiment"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

// 方針の提案の適用 (issue #879): a Self-Improvement policy proposal applied or
// rolled back in either period moves the Policy Engine's thresholds, so the
// periods no longer differ only in the experiment's variable.

func proposalRow(t *testing.T, w *sql.DB, version, appliedAt, rolledBackAt any) {
	t.Helper()
	testkit.MustExec(t, w, `INSERT INTO policy_proposals (proposed_at, rationale_json, proposed_changes_json, status, applied_policy_version, applied_at, rolled_back_at)
		VALUES (?, '{}', '{}', 'applied', ?, ?, ?)`, testkit.Stored(t, beforeTime), version, stored(t, appliedAt), stored(t, rolledBackAt))
}

func stored(t *testing.T, at any) any {
	t.Helper()
	if s, ok := at.(string); ok {
		return testkit.Stored(t, s)
	}
	return nil
}

func TestAPolicyProposalAppliedInEitherPeriodConfounds(t *testing.T) {
	for name, tt := range map[string]struct {
		at, period string
	}{
		"before period": {midBefore, experiment.PeriodBefore},
		"after period":  {inside, experiment.PeriodAfter},
	} {
		t.Run(name, func(t *testing.T) {
			srv, w, id := settingExperiment(t)
			proposalRow(t, w, "sol-12", tt.at, nil)
			f := wantConfounded(t, confoundersOf(t, srv, id), experiment.ConfounderPolicy, "sol-12", tt.period)
			if len(f.Values) != 1 || f.Values[0] != experiment.PolicyApplied || f.At != tt.at {
				t.Errorf("confounder = %+v, want the application of sol-12 at %s", f, tt.at)
			}
		})
	}
}

func TestAPolicyProposalRolledBackInThePeriodConfounds(t *testing.T) {
	srv, w, id := settingExperiment(t)
	proposalRow(t, w, "sol-12", beforeTime, inside)
	f := wantConfounded(t, confoundersOf(t, srv, id), experiment.ConfounderPolicy, "sol-12", experiment.PeriodAfter)
	if len(f.Values) != 1 || f.Values[0] != experiment.PolicyRolledBack {
		t.Errorf("confounder = %+v, want a rollback", f)
	}
}

// A proposal applied before the before period (and not rolled back inside the
// periods) was in force throughout both, so it is the same on both sides.
func TestAPolicyProposalOutsideThePeriodsDoesNotConfound(t *testing.T) {
	srv, w, id := settingExperiment(t)
	proposalRow(t, w, "sol-11", beforeTime, afterTime)
	proposalRow(t, w, "sol-13", afterTime, nil)
	proposalRow(t, w, "sol-14", nil, nil) // proposed, never applied
	if c := confoundersOf(t, srv, id); c.Confounded || c.Verdict != experiment.VerdictImproved {
		t.Fatalf("proposals outside the periods confounded: verdict %s, %+v", c.Verdict, c.Confounders)
	}
}

// 同じ値での保存し直し: saving a setting again with the value it already had
// changed nothing and is not a confounder; a real change, also from "no stored
// value", still is.
func TestSavingAnotherSettingAgainWithTheSameValueIsNoConfounder(t *testing.T) {
	srv, w, id := settingExperiment(t)
	other := "risk.max_daily_trades"
	auditRow(t, w, inside, "set", other, "5", "5")
	auditRow(t, w, midBefore, "set", other, "0.5", "0.50") // as a number
	testkit.MustExec(t, w, `INSERT INTO mcp_settings_audit (at, actor, client, action, setting_key, before_value, after_value)
		VALUES (?, 'gui', '', 'reset', ?, NULL, NULL)`, testkit.Stored(t, inside), other) // a reset of a key that had no stored value
	if c := confoundersOf(t, srv, id); c.Confounded || c.Verdict != experiment.VerdictImproved {
		t.Fatalf("saves that changed nothing confounded: verdict %s, %+v", c.Verdict, c.Confounders)
	}

	testkit.MustExec(t, w, `INSERT INTO mcp_settings_audit (at, actor, client, action, setting_key, before_value, after_value)
		VALUES (?, 'gui', '', 'set', ?, NULL, '3')`, testkit.Stored(t, inside), other)
	wantConfounded(t, confoundersOf(t, srv, id), experiment.KindSetting, other, experiment.PeriodAfter)
}
