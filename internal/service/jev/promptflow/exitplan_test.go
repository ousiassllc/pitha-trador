package promptflow_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/execution"
	"github.com/ousiassllc/pitha-trador/internal/service/jev"
)

// Issue #925: trader-v5 is the trader-v3 question group with exactly two
// Instructions rewritten (direction and continuation_probability); the live
// default stays trader-v3.
func TestTraderV5DiffersFromV3OnlyInDirectionAndContinuationInstructions(t *testing.T) {
	if jev.TraderQuestionVersion != "trader-v3" || jev.TraderExitPlanCandidateVersion != "trader-v5" {
		t.Fatalf("live default %q / candidate %q, want trader-v3 / trader-v5", jev.TraderQuestionVersion, jev.TraderExitPlanCandidateVersion)
	}
	v3, _ := jev.TraderQuestionsFor("trader-v3")
	v5, err := jev.TraderQuestionsFor("trader-v5")
	if err != nil || len(v3) != len(v5) {
		t.Fatalf("trader-v5 = %d questions, %v; want the %d of trader-v3", len(v5), err, len(v3))
	}
	for id, q := range v3 {
		same := string(mustJSON(t, q)) == string(mustJSON(t, v5[id]))
		changed := id == "direction" || id == "continuation_probability"
		if changed == same {
			t.Errorf("question %q: identical to trader-v3 = %v, want %v", id, same, !changed)
		}
		if changed && (v5[id].Type != q.Type || string(mustJSON(t, v5[id].Criteria)) != string(mustJSON(t, q.Criteria))) {
			t.Errorf("question %q: only the Instructions may change, the type and the options/criteria are trader-v3's", id)
		}
	}
}

// The numbers in the wording are the ones execution.DefaultConfig() closes a
// Paper position with (FR-EXIT-2), and the old horizon phrase is gone.
func TestTraderV5NamesTheExitsOfExecutionDefaultConfig(t *testing.T) {
	cfg := execution.DefaultConfig()
	v5, _ := jev.TraderQuestionsFor("trader-v5")
	for id, wants := range map[string][]string{
		"direction": {
			"entered at the ask", "take profit at +1.2% (120 bps)", "stop loss at -0.6% (60 bps)",
			"trailing stop 0.5% below", "forced exit 20 minutes", "round-trip cost",
		},
		"continuation_probability": {"a LONG enters at the ask", "take profit (+1.2%)", "stop loss (-0.6%)", "0.5% trailing stop", "20-minute limit", "round-trip cost"},
	} {
		got := v5[id].Instructions
		for _, want := range wants {
			if !strings.Contains(got, want) {
				t.Errorf("%s: the trader-v5 Instructions lack %q", id, want)
			}
		}
		if strings.Contains(got, "next several minutes") {
			t.Errorf("%s: the trader-v5 Instructions still say \"next several minutes\"", id)
		}
	}
	// The expected figures above must be the code's, not a copy that can drift.
	if got := fmt.Sprintf("+%g%% -%g%% %g%% %d", cfg.TakeProfitPct, cfg.StopLossPct, cfg.TrailingStopPct, cfg.MaxHoldingMinutes); got != "+1.2% -0.6% 0.5% 20" {
		t.Errorf("execution.DefaultConfig() exits = %s: the trader-v5 wording was written for +1.2%% -0.6%% 0.5%% 20; add a new version", got)
	}
}
