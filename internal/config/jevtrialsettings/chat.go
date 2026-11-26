package jevtrialsettings

import "math"

// EndpointChatCompletions is the chat Completions path (issue #1224): the arm
// asks a general chat model the FR-TRADER-1 questions as chat messages with a
// structured JSON output instead of sending System One's {state, questions}. It
// equals trialwire.ChatCompletionsEndpoint (a test in internal/service/jev/trialflow
// pins that). It is relative to the provider's base URL, which for OpenRouter is
// https://openrouter.ai/api, so it is not "/api/v1/chat/completions".
const EndpointChatCompletions = "/v1/chat/completions"

// Worst-case list prices of a chat arm, in USD per million tokens. A chat model
// prices its output as well as its input, and its input price is far above
// WorstCaseUSDPerMtok (0.24, the stage-1 decision models'). A chat arm whose route
// is not pinned is counted at no less than these, so a low configured price cannot
// loosen a cap (issue #1224, the same reasoning as issue #1065).
//
// Source: the highest published prices of the general chat models the trial
// compares (openai/gpt-6.2-astra: 8 USD input, 40 USD output per million tokens;
// anthropic/claude-opus-5.5 is 4 / 20 and google/gemini-4-argon 2 / 10), read
// from OpenRouter's model list (GET /api/v1/models) on 2026-11-26. They are a
// counting rule for the caps, not a measured bill: a dearer model needs a higher
// value here (the spec's "費用の数え方" names this constant).
const (
	ChatWorstInputUSDPerMtok  = 8.0
	ChatWorstOutputUSDPerMtok = 40.0
)

// IsChat reports whether the arm calls the chat Completions API.
func (a Arm) IsChat() bool { return a.Endpoint == EndpointChatCompletions }

// ChatCountingUSDPerMtok is the input and the output price per million tokens the
// daily caps count a chat arm's call at when its response reports no cost (and
// that its reservation and its failures are counted at): the configured list
// prices for an arm whose route is pinned (RoutePinned), otherwise never below
// ChatWorstInputUSDPerMtok / ChatWorstOutputUSDPerMtok. A price that is not set
// (a chat arm always sets both; Validate) is counted at the worst case.
func (a Arm) ChatCountingUSDPerMtok() (input, output float64) {
	return chatCounting(a.ListPriceUSDPerMtok, ChatWorstInputUSDPerMtok, a.RoutePinned()),
		chatCounting(a.ListPriceOutputUSDPerMtok, ChatWorstOutputUSDPerMtok, a.RoutePinned())
}

func chatCounting(listed, worst float64, pinned bool) float64 {
	if listed <= 0 {
		return worst
	}
	if pinned {
		return listed
	}
	return max(listed, worst)
}

// validateChatPrices checks the list prices against the endpoint: a chat arm must
// set both (0 and omitted are refused: its cost is not input-only, so an unset
// price would be guessed), and only a chat arm has an output price.
func (a Arm) validateChatPrices() error {
	if p := a.ListPriceOutputUSDPerMtok; math.IsNaN(p) || math.IsInf(p, 0) || p < 0 || p > MaxListPriceUSDPerMtok {
		return armsErr(ArmsErrListPriceOutput, a.ID, "arm %q: list_price_output_usd_per_mtok must be 0〜%g", a.ID, MaxListPriceUSDPerMtok)
	}
	if !a.IsChat() {
		if a.ListPriceOutputUSDPerMtok != 0 {
			return armsErr(ArmsErrListPriceOutput, a.ID, "arm %q: list_price_output_usd_per_mtok is only for endpoint %q", a.ID, EndpointChatCompletions)
		}
		return nil
	}
	if a.ListPriceUSDPerMtok <= 0 {
		return armsErr(ArmsErrListPrice, a.ID, "arm %q: endpoint %q needs list_price_usd_per_mtok above 0 (USD per million input tokens)", a.ID, EndpointChatCompletions)
	}
	if a.ListPriceOutputUSDPerMtok <= 0 {
		return armsErr(ArmsErrListPriceOutput, a.ID, "arm %q: endpoint %q needs list_price_output_usd_per_mtok above 0 (USD per million output tokens)", a.ID, EndpointChatCompletions)
	}
	return nil
}
