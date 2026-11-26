package replayflow_test

import (
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/jevtrialsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/trial"
)

// A replay estimate reserves a chat arm by the chat body and input plus output (issue
// #1224), and gives no list-price estimate for it (the output is unknown before the
// call); a stage-1 arm next to it keeps its input-only reservation.
func TestPlan_ChatArmIsReservedOnInputAndOutput(t *testing.T) {
	e := newEnv(t)
	e.enable()
	chat := arm("chat", "")
	chat.Endpoint = jevtrialsettings.EndpointChatCompletions
	chat.ListPriceUSDPerMtok, chat.ListPriceOutputUSDPerMtok = 2, 10
	stage1 := arm("stage1", "")
	stage1.ListPriceUSDPerMtok = 0.038
	e.configure(0, chat, stage1)
	e.decide(time.Date(2026, 10, 5, 1, 0, 0, 0, time.UTC), state(1))
	_, plan := e.start(wholeOctober(10))

	reserved := map[int]float64{}
	for _, c := range plan.Calls {
		reserved[c.Arm] = c.ReservedUSD
	}
	if len(plan.Calls) != 2 || reserved[0] <= reserved[1]*10 {
		t.Fatalf("reservations = %v, want the chat call's far above the stage-1 call's", reserved)
	}
	for i, a := range plan.Arms {
		switch a.Arm.ID {
		case "chat":
			if a.ListPriceUSD != nil {
				t.Errorf("arm %d (chat) has a list-price estimate %v, want none", i, *a.ListPriceUSD)
			}
			want := trial.ReservedUSD(a.Arm, plan.Calls[i].RequestSize)
			if a.MaxCostUSD != want {
				t.Errorf("chat MaxCostUSD = %v, want %v", a.MaxCostUSD, want)
			}
		case "stage1":
			if a.ListPriceUSD == nil {
				t.Error("the stage-1 arm lost its list-price estimate")
			}
		}
	}
	if plan.ListPriceUSD != nil {
		t.Errorf("plan list price = %v, want none while a chat arm is in it", *plan.ListPriceUSD)
	}
}
