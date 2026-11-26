package trial_test

import (
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/jevtrialsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain/jevtrial"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/shadow"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/trial"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/trialwire"
)

// chatArm is a chat arm priced 2 USD input / 10 USD output per million tokens, below
// the chat worst case (8 / 40), so the counting rule shows.
func chatArm(pinned bool) jevtrialsettings.Arm {
	a := jevtrialsettings.Arm{ID: "chat", Provider: "openrouter", Model: "google/gemini-4-argon", PromptVersion: "trader-v3",
		Endpoint: jevtrialsettings.EndpointChatCompletions, ListPriceUSDPerMtok: 2, ListPriceOutputUSDPerMtok: 10}
	if pinned {
		a.ProviderOnly = []string{"google-vertex"}
	}
	return a
}

const (
	worstIn  = jevtrialsettings.ChatWorstInputUSDPerMtok
	worstOut = jevtrialsettings.ChatWorstOutputUSDPerMtok
	maxOut   = trialwire.ChatMaxOutputTokens
)

// usage.cost wins over everything for a chat arm, as for any other.
func TestChatAnsweredCost_ReportedWins(t *testing.T) {
	c := trial.AnsweredCost(chatArm(false), trialwire.Response{ReportedCostUSD: new(0.0123), InputTokens: 1500, OutputTokens: 90}, 6000)
	if c.Source != jevtrial.CostReported || c.USD == nil || *c.USD != 0.0123 || c.CountedUSD != 0.0123 {
		t.Errorf("cost = %+v, want reported 0.0123 stored and counted", c)
	}
}

// Without a reported cost, input and output tokens are both priced: the stored estimate
// at the list prices, the caps' count at the counting prices (the chat worst case unless
// the route is pinned).
func TestChatAnsweredCost_ListPriceEstimateOnInputAndOutput(t *testing.T) {
	resp := trialwire.Response{InputTokens: 1500, OutputTokens: 90}
	wantUSD := (1500*2.0 + 90*10.0) / 1e6
	for name, tc := range map[string]struct {
		arm     jevtrialsettings.Arm
		counted float64
	}{
		"not pinned: counted at the chat worst case": {chatArm(false), (1500*worstIn + 90*worstOut) / 1e6},
		"pinned: counted at the list prices":         {chatArm(true), wantUSD},
	} {
		c := trial.AnsweredCost(tc.arm, resp, 6000)
		if c.Source != jevtrial.CostListPriceEstimate || c.USD == nil || !near(*c.USD, wantUSD) || !near(c.CountedUSD, tc.counted) {
			t.Errorf("%s: cost = %+v (usd %v), want estimate %v counted %v", name, c, c.USD, wantUSD, tc.counted)
		}
	}
}

// A response without token counts is never counted as 0 USD: the input is estimated from
// the request's size and the output is counted at the request's output limit.
func TestChatAnsweredCost_MissingTokensAreCountedAtTheLimits(t *testing.T) {
	const bytes = 6000
	in := shadow.EstimateInputTokens(bytes)
	c := trial.AnsweredCost(chatArm(false), trialwire.Response{}, bytes)
	if want := (float64(in)*worstIn + maxOut*worstOut) / 1e6; !near(c.CountedUSD, want) || c.CountedUSD <= 0 {
		t.Errorf("counted = %v, want %v (estimated input plus %d output tokens at the worst case)", c.CountedUSD, want, maxOut)
	}
}

// A failure counts nothing when the vendor refused before running the model (4xx other
// than 413, 529), and the call's whole reservation (input worst case plus the output
// limit) otherwise.
func TestFailedCostFor_ChatCountsInputAndOutput(t *testing.T) {
	const bytes = 6000
	arm := chatArm(false)
	reserved := trial.ReservedUSD(arm, bytes)
	for _, status := range []int{400, 401, 404, 422, 429, 529} {
		if c := trial.FailedCostFor(arm, status, bytes); c.CountedUSD != 0 || c.USD != nil || c.Source != jevtrial.CostUnknown {
			t.Errorf("status %d: cost = %+v, want unknown and 0 counted", status, c)
		}
	}
	for _, status := range []int{0, 413, 500, 502, 503} {
		if c := trial.FailedCostFor(arm, status, bytes); !near(c.CountedUSD, reserved) || c.USD != nil || c.Source != jevtrial.CostUnknown {
			t.Errorf("status %d: cost = %+v, want unknown and the reservation %v counted", status, c, reserved)
		}
	}
}

// The reservation of a chat call holds the output limit as well as the input of the body
// that is actually sent; a stage-1 arm's stays input-only at 0.24, the same as before.
func TestReservedUSD_ChatIncludesTheOutputLimitAndStage1IsUnchanged(t *testing.T) {
	const bytes = 6000
	in := shadow.EstimateInputTokens(bytes)
	got := trial.ReservedUSD(chatArm(false), bytes)
	if want := (float64(in)*worstIn + maxOut*worstOut) / 1e6; !near(got, want) {
		t.Errorf("chat reservation = %v, want %v", got, want)
	}
	if outputOnly := maxOut * worstOut / 1e6; got <= outputOnly {
		t.Errorf("chat reservation %v does not exceed its output limit alone (%v)", got, outputOnly)
	}
	if pinned := trial.ReservedUSD(chatArm(true), bytes); !near(pinned, (float64(in)*2+maxOut*10)/1e6) {
		t.Errorf("pinned chat reservation = %v, want it at the list prices", pinned)
	}

	for _, arm := range []jevtrialsettings.Arm{
		{ListPriceUSDPerMtok: 0.038},
		{ListPriceUSDPerMtok: 5, Endpoint: jevtrialsettings.EndpointDecisions},
		{Endpoint: jevtrialsettings.EndpointSystemOne},
	} {
		want := float64(in) * 0.24 / 1e6
		if got := trial.ReservedUSD(arm, bytes); !near(got, want) {
			t.Errorf("stage-1 reservation of %+v = %v, want %v (input only at 0.24)", arm, got, want)
		}
		if c := trial.FailedCostFor(arm, 500, bytes); c != trial.FailedCost(500, bytes) || !near(c.CountedUSD, want) {
			t.Errorf("stage-1 failure of %+v = %+v, want the unchanged FailedCost %+v", arm, c, trial.FailedCost(500, bytes))
		}
	}
	// The same response from a stage-1 arm is still priced on input only.
	c := trial.AnsweredCost(jevtrialsettings.Arm{ListPriceUSDPerMtok: 0.5}, trialwire.Response{InputTokens: 1000, OutputTokens: 5000}, bytes)
	if c.Source != jevtrial.CostListPriceEstimate || c.USD == nil || !near(*c.USD, 1000*0.5/1e6) || !near(c.CountedUSD, 1000*0.5/1e6) {
		t.Errorf("stage-1 answered cost = %+v, want input tokens only", c)
	}
}

// The request size a chat arm is sized and reserved by is its chat body's, not System One's.
func TestRawRequestBytesFor_ChatBodyDiffersFromSystemOne(t *testing.T) {
	state := []byte(`{"symbol":"7203"}`)
	chat, err := trial.RawRequestBytesFor(chatArm(false), "trader-v3", state)
	if err != nil {
		t.Fatal(err)
	}
	stage1 := chatArm(false)
	stage1.Endpoint = ""
	systemOne, err := trial.RawRequestBytesFor(stage1, "trader-v3", state)
	if err != nil {
		t.Fatal(err)
	}
	if chat == systemOne {
		t.Errorf("chat and System One bodies have the same size %d", chat)
	}
}
