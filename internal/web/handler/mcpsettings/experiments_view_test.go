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
