package trialwire_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/jev"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/systemone"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/trialwire"
)

// chatEnvelope is a chat Completions response (OpenRouter shape, issue #1224) whose
// message content is content, with usage.cost.
func chatEnvelope(content string) string {
	quoted, _ := json.Marshal(content)
	return `{"id":"gen-chat-1","model":"openai/gpt-6.2-astra-20261101","provider":"OpenAI",` +
		`"choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":` + string(quoted) + `}}],` +
		`"usage":{"prompt_tokens":1500,"completion_tokens":90,"cost":0.0123}}`
}

const chatAnswers = `{"direction":{"choice":"SHORT","confidence":0.66},"regime":{"choice":"BREAKOUT","confidence":0.7},` +
	`"entry_quality":{"choice":"good","confidence":0.5},"toxic_flow":0.12,"liquidity_stressed":0.07,"continuation_probability":0.58}`

func chatClient(server string, provider *systemone.Provider) *trialwire.Client {
	return trialwire.New(trialwire.Config{
		BaseURL: server, APIKey: "sk-test", Model: "openai/gpt-6.2-astra", Endpoint: trialwire.ChatCompletionsEndpoint, Provider: provider,
	})
}

// A chat arm posts to /v1/chat/completions with chat messages and a structured
// output, never System One's {state, questions}.
func TestChat_RequestIsAChatBodyWithAStructuredOutput(t *testing.T) {
	server, got := newServer(t, http.StatusOK, chatEnvelope(chatAnswers))
	provider := &systemone.Provider{Only: []string{"openai"}, AllowFallbacks: new(false), DataCollection: "deny", ZDR: new(true)}
	if _, _, _, err := chatClient(server.URL, provider).Trader(context.Background(), traderRequest()); err != nil {
		t.Fatalf("Trader: %v", err)
	}
	if got.path != "/v1/chat/completions" || got.auth != "Bearer sk-test" {
		t.Errorf("path = %q auth set = %v", got.path, got.auth == "Bearer sk-test")
	}
	for _, key := range []string{"state", "questions"} {
		if _, ok := got.body[key]; ok {
			t.Errorf("the chat body has System One's top-level %q", key)
		}
	}
	var messages []struct{ Role, Content string }
	if err := json.Unmarshal(got.body["messages"], &messages); err != nil || len(messages) != 2 || messages[0].Role != "system" || messages[1].Role != "user" {
		t.Fatalf("messages = %s (%v), want a system and a user message", got.body["messages"], err)
	}
	for _, want := range []string{"[direction]", "[entry_quality]", "- LONG:", "Answer a probability from 0 to 1", "State (JSON):", `"similar_past_cases"`} {
		if !strings.Contains(messages[1].Content, want) {
			t.Errorf("the user message lacks %q", want)
		}
	}
	var format struct {
		Type       string `json:"type"`
		JSONSchema struct {
			Name   string `json:"name"`
			Strict bool   `json:"strict"`
			Schema struct {
				Required             []string                   `json:"required"`
				AdditionalProperties bool                       `json:"additionalProperties"`
				Properties           map[string]json.RawMessage `json:"properties"`
			} `json:"schema"`
		} `json:"json_schema"`
	}
	if err := json.Unmarshal(got.body["response_format"], &format); err != nil {
		t.Fatal(err)
	}
	wantIDs := []string{"continuation_probability", "direction", "entry_quality", "liquidity_stressed", "regime", "toxic_flow"}
	if format.Type != "json_schema" || !format.JSONSchema.Strict || !strings.Contains(format.JSONSchema.Name, trialwire.ChatProtocolVersion) ||
		format.JSONSchema.Schema.AdditionalProperties || strings.Join(format.JSONSchema.Schema.Required, ",") != strings.Join(wantIDs, ",") || len(format.JSONSchema.Schema.Properties) != len(wantIDs) {
		t.Errorf("response_format = %+v, want a strict json_schema (named with %s) requiring %v and closed to other keys", format, trialwire.ChatProtocolVersion, wantIDs)
	}
	if string(got.body["max_tokens"]) != "1024" || trialwire.ChatMaxOutputTokens != 1024 || string(got.body["temperature"]) != "0" || string(got.body["model"]) != `"openai/gpt-6.2-astra"` {
		t.Errorf("max_tokens/temperature/model = %s/%s/%s", got.body["max_tokens"], got.body["temperature"], got.body["model"])
	}
	var routing map[string]json.RawMessage
	if err := json.Unmarshal(got.body["provider"], &routing); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{"only": `["openai"]`, "allow_fallbacks": "false", "data_collection": `"deny"`, "zdr": "true"} {
		if string(routing[key]) != want {
			t.Errorf("provider.%s = %s, want %s", key, routing[key], want)
		}
	}
	// require_parameters would leave a model that does not list temperature (e.g.
	// claude-opus-5.5, gpt-6.2-astra) without an endpoint: it is not sent.
	if _, ok := routing["require_parameters"]; ok || len(routing) != 4 {
		t.Errorf("provider = %v, want only, allow_fallbacks, data_collection and zdr", routing)
	}
}

// A custom arm has no routing preference: the provider object is left out.
func TestChat_NoRoutingPreferenceForACustomArm(t *testing.T) {
	server, got := newServer(t, http.StatusOK, chatEnvelope(chatAnswers))
	if _, _, _, err := chatClient(server.URL, nil).Trader(context.Background(), traderRequest()); err != nil {
		t.Fatalf("Trader: %v", err)
	}
	if _, ok := got.body["provider"]; ok {
		t.Errorf("provider sent without a routing preference: %s", got.body["provider"])
	}
}

func TestChat_ReadsTheAnswersUsageCostAndServedFields(t *testing.T) {
	server, _ := newServer(t, http.StatusOK, chatEnvelope(chatAnswers))
	resp, _, bytes, err := chatClient(server.URL, nil).Trader(context.Background(), traderRequest())
	if err != nil {
		t.Fatalf("Trader: %v", err)
	}
	if resp.Direction != "SHORT" || resp.Regime != "BREAKOUT" || resp.EntryQuality != "good" || len(resp.Refused) != 0 {
		t.Errorf("answers = %q/%q/%q refused %v", resp.Direction, resp.Regime, resp.EntryQuality, resp.Refused)
	}
	if resp.Confidence == nil || *resp.Confidence != 0.66 || resp.ToxicFlow == nil || *resp.ToxicFlow != 0.12 || *resp.ContinuationProbability != 0.58 {
		t.Errorf("confidence/noul = %v/%v/%v", resp.Confidence, resp.ToxicFlow, resp.ContinuationProbability)
	}
	if resp.ModelID != "openai/gpt-6.2-astra-20261101" || resp.Provider != "OpenAI" || resp.ResponseID != "gen-chat-1" {
		t.Errorf("served model/provider/id = %q/%q/%q", resp.ModelID, resp.Provider, resp.ResponseID)
	}
	if resp.InputTokens != 1500 || resp.OutputTokens != 90 || resp.ReportedCostUSD == nil || *resp.ReportedCostUSD != 0.0123 || bytes == 0 {
		t.Errorf("usage = %d/%d cost %v request bytes %d", resp.InputTokens, resp.OutputTokens, resp.ReportedCostUSD, bytes)
	}
	if !strings.Contains(resp.Body, `"gen-chat-1"`) {
		t.Errorf("the raw response body is not kept: %q", resp.Body)
	}
}

// A usage without cost is "not reported" (nil), never 0 USD.
func TestChat_AnAbsentCostIsNilNotZero(t *testing.T) {
	reply := strings.Replace(chatEnvelope(chatAnswers), `,"cost":0.0123`, "", 1)
	server, _ := newServer(t, http.StatusOK, reply)
	resp, _, _, err := chatClient(server.URL, nil).Trader(context.Background(), traderRequest())
	if err != nil || resp.ReportedCostUSD != nil {
		t.Fatalf("reported cost = %v (%v), want nil", resp.ReportedCostUSD, err)
	}
}

// A null answer is a refusal, kept NULL (not NONE and not 0); one is partial, all of
// them refused.
func TestChat_NullAnswersAreRefusalsNotValues(t *testing.T) {
	partial := `{"direction":null,"regime":{"choice":"RANGE","confidence":0.4},"entry_quality":{"choice":"poor","confidence":0.3},` +
		`"toxic_flow":null,"liquidity_stressed":0.5,"continuation_probability":0.5}`
	server, _ := newServer(t, http.StatusOK, chatEnvelope(partial))
	resp, _, _, err := chatClient(server.URL, nil).Trader(context.Background(), traderRequest())
	if err != nil {
		t.Fatalf("Trader: %v", err)
	}
	if resp.Direction != "" || resp.Confidence != nil || resp.ToxicFlow != nil || strings.Join(resp.Refused, ",") != "direction,toxic_flow" {
		t.Errorf("direction = %q confidence = %v toxic_flow = %v refused = %v", resp.Direction, resp.Confidence, resp.ToxicFlow, resp.Refused)
	}
	if resp.Regime != "RANGE" || resp.LiquidityStressed == nil || *resp.LiquidityStressed != 0.5 {
		t.Errorf("the answered questions were lost: %q %v", resp.Regime, resp.LiquidityStressed)
	}

	all := `{"direction":null,"regime":null,"entry_quality":null,"toxic_flow":null,"liquidity_stressed":null,"continuation_probability":null}`
	server, _ = newServer(t, http.StatusOK, chatEnvelope(all))
	resp, _, _, err = chatClient(server.URL, nil).Trader(context.Background(), traderRequest())
	if err != nil || len(resp.Refused) != 6 || resp.Direction != "" || resp.EntryQuality != "" {
		t.Fatalf("all null: refused = %v direction = %q (%v), want 6 refusals", resp.Refused, resp.Direction, err)
	}

	none := strings.Replace(chatAnswers, `"choice":"SHORT"`, `"choice":"NONE"`, 1)
	server, _ = newServer(t, http.StatusOK, chatEnvelope(none))
	resp, _, _, err = chatClient(server.URL, nil).Trader(context.Background(), traderRequest())
	if err != nil || resp.Direction != "NONE" || len(resp.Refused) != 0 {
		t.Fatalf("NONE: direction = %q refused = %v (%v), want an answer of NONE that is not a refusal", resp.Direction, resp.Refused, err)
	}
}

// Whatever is not a valid answer set is systemone.ErrInvalidResponse: nothing is
// guessed from free text.
func TestChat_AnInvalidMessageIsErrInvalidResponse(t *testing.T) {
	replace := func(old, with string) string { return strings.Replace(chatAnswers, old, with, 1) }
	for name, reply := range map[string]string{
		"free text":               chatEnvelope("I think this is a long."),
		"fenced json":             chatEnvelope("```json\n" + chatAnswers + "\n```"),
		"json array":              chatEnvelope("[1,2]"),
		"empty content":           chatEnvelope(""),
		"null content":            `{"model":"m","choices":[{"message":{"role":"assistant","content":null,"refusal":"no"}}]}`,
		"no choices":              `{"model":"m","choices":[]}`,
		"error body with 200":     `{"error":{"code":502,"message":"upstream"}}`,
		"not json":                `<html>`,
		"cut off":                 chatEnvelope(chatAnswers[:80]),
		"missing question":        chatEnvelope(replace(`"toxic_flow":0.12,`, "")),
		"unasked question":        chatEnvelope(strings.TrimSuffix(chatAnswers, "}") + `,"extra":1}`),
		"noul out of range":       chatEnvelope(replace(`"toxic_flow":0.12`, `"toxic_flow":1.5`)),
		"noul as a string":        chatEnvelope(replace(`"toxic_flow":0.12`, `"toxic_flow":"high"`)),
		"undefined choice":        chatEnvelope(replace(`"choice":"SHORT"`, `"choice":"MAYBE"`)),
		"confidence out of range": chatEnvelope(replace(`"confidence":0.66`, `"confidence":2`)),
		"choice as a string":      chatEnvelope(replace(`{"choice":"SHORT","confidence":0.66}`, `"SHORT"`)),
		"no model":                strings.Replace(chatEnvelope(chatAnswers), `"model":"openai/gpt-6.2-astra-20261101",`, "", 1),
	} {
		server, got := newServer(t, http.StatusOK, reply)
		_, _, bytes, err := chatClient(server.URL, nil).Trader(context.Background(), traderRequest())
		if !errors.Is(err, systemone.ErrInvalidResponse) {
			t.Errorf("%s: err = %v, want systemone.ErrInvalidResponse", name, err)
		}
		if bytes == 0 || got.calls.Load() != 1 {
			t.Errorf("%s: request bytes = %d, calls = %d, want the size of the body sent and one attempt", name, bytes, got.calls.Load())
		}
	}
}

// An HTTP error is a *jev.APIError with its status (one attempt, no retry).
func TestChat_AnHTTPErrorKeepsItsStatus(t *testing.T) {
	server, got := newServer(t, http.StatusBadRequest, `{"error":{"code":400,"message":"model does not support response_format"}}`)
	_, _, bytes, err := chatClient(server.URL, nil).Trader(context.Background(), traderRequest())
	var apiErr *jev.APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusBadRequest || got.calls.Load() != 1 || bytes == 0 {
		t.Fatalf("err = %v, calls = %d, request bytes = %d, want a 400 APIError after one attempt", err, got.calls.Load(), bytes)
	}
}

// The size the trial reserves from is the size of the body that is sent, for a live
// request and for a replayed one, and differs from System One's.
func TestChatRequestBytes_EqualsTheSentBody(t *testing.T) {
	server, _ := newServer(t, http.StatusOK, chatEnvelope(chatAnswers))
	provider := &systemone.Provider{DataCollection: "deny"}
	client := chatClient(server.URL, provider)
	_, _, sent, err := client.Trader(context.Background(), traderRequest())
	if err != nil {
		t.Fatal(err)
	}
	size, err := trialwire.ChatRequestBytes("openai/gpt-6.2-astra", provider, traderRequest())
	if err != nil || size != sent {
		t.Errorf("ChatRequestBytes = %d (%v), want the sent %d", size, err, sent)
	}
	if systemOne, _ := trialwire.RequestBytes("openai/gpt-6.2-astra", provider, traderRequest()); systemOne == size {
		t.Errorf("the chat body is the size of System One's (%d)", size)
	}
	stored := json.RawMessage(`{"symbol":"7203","price":2100}`)
	_, _, sentRaw, err := client.TraderRaw(context.Background(), jev.TraderQuestionVersion, stored)
	if err != nil {
		t.Fatal(err)
	}
	rawSize, err := trialwire.ChatRawRequestBytes("openai/gpt-6.2-astra", provider, jev.TraderQuestionVersion, stored)
	if err != nil || rawSize != sentRaw {
		t.Errorf("ChatRawRequestBytes = %d (%v), want the sent %d", rawSize, err, sentRaw)
	}
	if _, err := trialwire.ChatRequestBytes("m", nil, jev.TraderRequest{QuestionVersion: "no-such-version"}); err == nil {
		t.Error("an unregistered prompt version was sized")
	}
}

// Every registered prompt version's questions can be asked as chat messages with a
// schema (no new prompt version is made for the chat wire).
func TestChat_AsksEveryRegisteredVersionsQuestions(t *testing.T) {
	for _, version := range jev.TraderVersions() {
		req := traderRequest()
		req.QuestionVersion = version
		if size, err := trialwire.ChatRequestBytes("m", nil, req); err != nil || size == 0 {
			t.Errorf("%s: ChatRequestBytes = %d, %v", version, size, err)
		}
	}
}
