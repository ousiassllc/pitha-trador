package mcpsettings

import (
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/experiment"
)

// issue #885 / #886: a recorded JEV_MODEL switch says which period it was
// saved in, like the other confounders.
func TestConfounderLine_ModelSwitchNamesItsPeriod(t *testing.T) {
	for period, want := range map[string]string{
		experiment.PeriodBefore: "実験の前の期間に",
		experiment.PeriodAfter:  "実験中（後の期間）に",
	} {
		line := confounderLine(experiment.Confounder{
			Kind: experiment.KindModel, Period: period, Name: experiment.ModelSwitchName,
			At: "2026-10-02T10:00:00Z", Values: []string{"model-a", "model-b"},
		})
		if !strings.Contains(line, want) || !strings.Contains(line, "model-a → model-b") {
			t.Errorf("period %s: line = %q, want it to contain %q and the switch", period, line, want)
		}
	}
}

// issue #879: a Self-Improvement policy proposal is a confounder that names its
// version, what happened to it and the period.
func TestConfounderLine_PolicyProposal(t *testing.T) {
	applied := confounderLine(experiment.Confounder{
		Kind: experiment.ConfounderPolicy, Period: experiment.PeriodBefore, Name: "sol-12",
		At: "2026-10-02T10:00:00Z", Values: []string{experiment.PolicyApplied},
	})
	for _, want := range []string{"実験の前の期間に", "方針の提案 sol-12", "適用されました", "2026-10-02 19:00"} {
		if !strings.Contains(applied, want) {
			t.Errorf("applied line = %q, want %q", applied, want)
		}
	}
	rolled := confounderLine(experiment.Confounder{
		Kind: experiment.ConfounderPolicy, Period: experiment.PeriodAfter, At: "2026-10-02T10:00:00Z", Values: []string{experiment.PolicyRolledBack},
	})
	if !strings.Contains(rolled, "取り消されました") || !strings.Contains(rolled, "方針の提案 が") {
		t.Errorf("rolled back line = %q", rolled)
	}
}

// issue #879: an unconfirmed switch is a verdict of its own, and the detail of
// the check is what the comparison shows as its explanation.
func TestSwitchUnconfirmedHasALabelAndShowsTheCheck(t *testing.T) {
	if got := verdictLabel(experiment.VerdictSwitchUnconfirmed); !strings.Contains(got, "切り替えを確認できない") {
		t.Errorf("label = %q", got)
	}
	p := detailProps(experiment.Comparison{
		Verdict: experiment.VerdictSwitchUnconfirmed,
		Switch:  experiment.SwitchCheck{Status: experiment.SwitchNotSwitched, Detail: "値が変わっていません。"},
	})
	if p.VerdictHelp != "値が変わっていません。" {
		t.Errorf("help = %q, want the switch check's detail", p.VerdictHelp)
	}
}

// issue #880: a row that failed shows the reason in place of a verdict.
func TestExperimentsPropsShowARowsError(t *testing.T) {
	props := experimentsProps([]experiment.Summary{
		{Experiment: experiment.Experiment{ID: 2, Status: experiment.StatusRunning}, Error: experiment.SummaryLimitExceeded, BeforeTrades: 10, AfterTrades: 5001},
		{Experiment: experiment.Experiment{ID: 1, Status: experiment.StatusClosed}, Verdict: experiment.VerdictImproved, Judged: true},
		{Experiment: experiment.Experiment{ID: 3, Status: experiment.StatusClosed}, Error: experiment.SummaryFailed},
	}, "")
	if len(props.Rows) != 3 {
		t.Fatalf("%d rows, want all three", len(props.Rows))
	}
	if r := props.Rows[0]; !strings.Contains(r.Error, "5,000") || r.Verdict != "" {
		t.Errorf("limit row = %+v", r)
	}
	if r := props.Rows[1]; r.Error != "" || r.VerdictLabel != "改善" {
		t.Errorf("intact row = %+v", r)
	}
	if r := props.Rows[2]; !strings.Contains(r.Error, "取得に失敗") {
		t.Errorf("failed row = %+v", r)
	}
}
