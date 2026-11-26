package trial

import (
	"encoding/json"
	"math"

	"github.com/ousiassllc/pitha-trador/internal/config/jevtrialsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain/jevtrial"
	"github.com/ousiassllc/pitha-trador/internal/service/jev"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/shadow"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/trialwire"
)

// The cost of a chat arm (jevtrialsettings.EndpointChatCompletions, issue #1224).
//
// The stage-1 rule (cost.go, reserve.go) prices input tokens only, at
// WorstCaseUSDPerMtok (0.24, the decision models' input price), on the premise that a
// trial's output is short. A general chat model prices its output too and its input
// far above 0.24, so a chat arm is counted on both sides instead, at
// Arm.ChatCountingUSDPerMtok (its list prices, not below the chat worst case
// unless its route is pinned). The stage-1 arms' counting is not touched.

// requestBytesFor is the size of the request body the arm's endpoint is sent for
// req: a chat arm's chat body, any other arm's System One body (the Codex arm's
// reservation stays at that size too, as before).
func requestBytesFor(arm jevtrialsettings.Arm, req jev.TraderRequest) (int, error) {
	if arm.IsChat() {
		return trialwire.ChatRequestBytes(arm.Model, providerFor(arm), req)
	}
	return trialwire.RequestBytes(arm.Model, providerFor(arm), req)
}

// RawRequestBytesFor is the size of the request body a replay of a saved input
// (trialwire.Client.TraderRaw) sends to arm.
func RawRequestBytesFor(arm jevtrialsettings.Arm, questionVersion string, stateJSON json.RawMessage) (int, error) {
	if arm.IsChat() {
		return trialwire.ChatRawRequestBytes(arm.Model, providerFor(arm), questionVersion, stateJSON)
	}
	return trialwire.RawRequestBytes(arm.Model, providerFor(arm), questionVersion, stateJSON)
}

// chatCountedUSD is what tokens in and out are counted at against the caps for a
// chat arm.
func chatCountedUSD(arm jevtrialsettings.Arm, inTokens, outTokens int) float64 {
	in, out := arm.ChatCountingUSDPerMtok()
	return (float64(inTokens)*in + float64(outTokens)*out) / 1e6
}

// chatReservedUSD is what a chat call of requestBytes is reserved at: the
// estimated input tokens of the request body actually sent, plus the most output
// the request lets the model bill (trialwire.ChatMaxOutputTokens), each at the
// arm's counting price. It is also what a failure that may have been billed is
// counted at (chatFailedCost).
func chatReservedUSD(arm jevtrialsettings.Arm, requestBytes int) float64 {
	return chatCountedUSD(arm, shadow.EstimateInputTokens(requestBytes), trialwire.ChatMaxOutputTokens)
}

// chatAnsweredCost prices a chat call that returned an answer whose response
// reported no cost: input tokens (the reported ones, else the estimate of the
// request's size) times the input list price plus output tokens times the output
// list price (source list_price_estimate). The caps count it at the arm's
// counting prices; when the response gave no output tokens, the output side is
// counted at the request's output limit, never 0 USD. An arm without both list
// prices (Validate refuses that) is unknown, counted at the reservation.
func chatAnsweredCost(arm jevtrialsettings.Arm, resp trialwire.Response, requestBytes int) Cost {
	in := resp.InputTokens
	if in <= 0 {
		in = shadow.EstimateInputTokens(requestBytes)
	}
	countedOut := resp.OutputTokens
	if countedOut <= 0 {
		countedOut = trialwire.ChatMaxOutputTokens
	}
	counted := chatCountedUSD(arm, in, countedOut)
	if arm.ListPriceUSDPerMtok <= 0 || arm.ListPriceOutputUSDPerMtok <= 0 {
		return Cost{Source: jevtrial.CostUnknown, CountedUSD: counted}
	}
	usd := (float64(in)*arm.ListPriceUSDPerMtok + float64(resp.OutputTokens)*arm.ListPriceOutputUSDPerMtok) / 1e6
	return Cost{USD: new(usd), Source: jevtrial.CostListPriceEstimate, CountedUSD: counted}
}

// reportedCost is the cost usage.cost reports, when the response gave a usable one.
func reportedCost(resp trialwire.Response) (float64, bool) {
	c := resp.ReportedCostUSD
	if c == nil || *c < 0 || math.IsNaN(*c) || math.IsInf(*c, 0) {
		return 0, false
	}
	return *c, true
}

// chatFailedCost counts a chat call that returned no answer: 0 for an answer the
// vendor refused before the model ran (the stage-1 rule, refusedBeforeRun), the
// call's reservation (input worst case plus the output limit) for any other
// failure, which may have been billed.
func chatFailedCost(arm jevtrialsettings.Arm, status, requestBytes int) Cost {
	if refusedBeforeRun(status) {
		return Cost{Source: jevtrial.CostUnknown}
	}
	return Cost{Source: jevtrial.CostUnknown, CountedUSD: chatReservedUSD(arm, requestBytes)}
}
