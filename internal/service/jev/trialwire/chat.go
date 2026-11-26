package trialwire

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ousiassllc/pitha-trador/internal/service/jev"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/systemone"
)

// The chat Completions wire (issue #1224): a general chat model is not a decision
// model, so it is not sent System One's {state, questions}. The FR-TRADER-1
// questions (the registered prompt version's, never a new one) are written into the
// messages, the answer is requested as a JSON object under response_format
// json_schema (OpenRouter Structured Outputs; checked against the OpenRouter docs on
// 2026-11-26), and the message's content is rewritten as a System One answer set
// and read with systemone.DecodeTrial, exactly as the Codex arm does
// (answers.go). The result is the same Response, so nothing downstream knows which
// wire answered.

// ChatProtocolVersion names the way the question group is put to a chat model (the
// messages, the schema and its name, and the request fields below). It is not a
// prompt version: the questions are the registered prompt version's. It is the chat
// wire's counterpart of Codex's codex-exec-v1, and a change of the layout must change
// it. It is known from the arm's endpoint, so no column stores it; it is part of the
// schema's name, so the provider's request log carries it.
const ChatProtocolVersion = "chat-completions-v1"

// ChatMaxOutputTokens is the max_tokens of every chat request. It bounds what one
// call can bill for its output and is the output side of the call's reservation
// (internal/service/jev/trial). The answer object is about 150 tokens; the rest is
// room for a model that reasons before it answers (its reasoning tokens count here).
// It is unmeasured: a model that needs more fails as invalid_response (the
// message is cut off) instead of billing more.
const ChatMaxOutputTokens = 1024

// chatSchemaName names the response_format schema (letters, digits, _ and - only).
const chatSchemaName = "trader_answers_" + ChatProtocolVersion

// chatPreamble is the system message of every chat request.
const chatPreamble = "You evaluate one market state for a trading research comparison. " +
	"Use only the state below. " +
	"Answer every question once and reply with a single JSON object that matches the given output schema and nothing else. " +
	"If you cannot or will not answer a question, answer null for it."

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatJSONSchema struct {
	Name   string          `json:"name"`
	Strict bool            `json:"strict"`
	Schema json.RawMessage `json:"schema"`
}

type chatResponseFormat struct {
	Type       string         `json:"type"`
	JSONSchema chatJSONSchema `json:"json_schema"`
}

type chatRequest struct {
	Model          string             `json:"model"`
	Messages       []chatMessage      `json:"messages"`
	ResponseFormat chatResponseFormat `json:"response_format"`
	MaxTokens      int                `json:"max_tokens"`
	// Temperature is always sent, 0: a comparison is reproducible only if the
	// models do not sample. A model that does not take it ignores it.
	Temperature float64 `json:"temperature"`
	// Provider is the routing preference (systemone.Provider: only, allow_fallbacks,
	// data_collection, zdr), the same as a System One request carries; nil sends none.
	// require_parameters is deliberately not sent: it keeps only endpoints that support
	// every parameter of the request, and temperature is one that some of the chat models
	// do not list (OpenRouter ignores it for them), so it would leave such a model no
	// endpoint at all. response_format is already a soft routing preference without it.
	Provider *systemone.Provider `json:"provider,omitempty"`
}

// chatBody is the body of the chat request that asks questions about state. A
// nil provider (an arm of the custom provider) sends no routing preference.
func chatBody(model string, provider *systemone.Provider, questions map[string]systemone.Question, state any) ([]byte, error) {
	qs, err := QuestionsPrompt(questions)
	if err != nil {
		return nil, err
	}
	st, err := StatePrompt(state)
	if err != nil {
		return nil, err
	}
	schema, err := AnswerSchema(questions)
	if err != nil {
		return nil, err
	}
	req := chatRequest{
		Model:    model,
		Provider: provider,
		Messages: []chatMessage{
			{Role: "system", Content: chatPreamble},
			{Role: "user", Content: qs + "\n" + st},
		},
		ResponseFormat: chatResponseFormat{Type: "json_schema", JSONSchema: chatJSONSchema{Name: chatSchemaName, Strict: true, Schema: schema}},
		MaxTokens:      ChatMaxOutputTokens,
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("trialwire: encode request: %w", err)
	}
	return body, nil
}

// ChatRequestBytes is the size of the chat request body Trader would send for req
// to an arm with model and provider, without sending anything (RequestBytes' chat
// counterpart: the trial reserves the call's worst-case cost from it).
func ChatRequestBytes(model string, provider *systemone.Provider, req jev.TraderRequest) (int, error) {
	questions, err := jev.TraderQuestionsFor(req.QuestionVersion)
	if err != nil {
		return 0, err
	}
	body, err := chatBody(model, provider, questions, StateOf(req))
	return len(body), err
}

// ChatRawRequestBytes is RawRequestBytes' chat counterpart.
func ChatRawRequestBytes(model string, provider *systemone.Provider, questionVersion string, stateJSON json.RawMessage) (int, error) {
	questions, err := jev.TraderQuestionsFor(questionVersion)
	if err != nil {
		return 0, err
	}
	body, err := chatBody(model, provider, questions, rawState{Market: stateJSON})
	return len(body), err
}

// askChat POSTs the chat request once and reads the answer from the message. A
// non-200 answer is a *jev.APIError; an answer whose message is not a valid answer
// set (not JSON, a question missing, a value out of range, a choice that is not an
// option) wraps systemone.ErrInvalidResponse.
func (c *Client) askChat(ctx context.Context, questions map[string]systemone.Question, state any) (Response, int, error) {
	body, err := chatBody(c.model, c.provider, questions, state)
	if err != nil {
		return Response{}, 0, err
	}
	respBody, err := c.post(ctx, body)
	if err != nil {
		return Response{}, len(body), err
	}
	resp, err := readChat(questions, respBody)
	return resp, len(body), err
}

// chatResponse is the part of a chat Completions response that is read. The
// fields the answer set does not depend on are kept raw, so one of an unexpected
// type is simply absent (as systemone's extras are) instead of failing the call.
type chatResponse struct {
	ID       json.RawMessage `json:"id"`
	Model    json.RawMessage `json:"model"`
	Provider json.RawMessage `json:"provider"`
	Choices  []struct {
		Message struct {
			Content *string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     json.RawMessage `json:"prompt_tokens"`
		CompletionTokens json.RawMessage `json:"completion_tokens"`
		Cost             json.RawMessage `json:"cost"`
	} `json:"usage"`
}

// readChat reads the body of a 200 chat response: choices[0].message.content is the
// answers (a JSON object keyed by question id), usage.prompt_tokens / completion_tokens
// the tokens and usage.cost the reported cost; the response's model, provider and id
// are kept as served. Nothing is guessed: a body without a message, a model or a
// JSON object of answers is systemone.ErrInvalidResponse.
func readChat(questions map[string]systemone.Question, respBody []byte) (Response, error) {
	var cr chatResponse
	if err := json.Unmarshal(respBody, &cr); err != nil {
		return Response{}, fmt.Errorf("%w: decode body: %w", systemone.ErrInvalidResponse, err)
	}
	if len(cr.Choices) == 0 || cr.Choices[0].Message.Content == nil {
		return Response{}, fmt.Errorf("%w: the response has no message content", systemone.ErrInvalidResponse)
	}
	usage := map[string]any{"input_tokens": intOf(cr.Usage.PromptTokens), "output_tokens": intOf(cr.Usage.CompletionTokens)}
	if len(cr.Usage.Cost) > 0 {
		usage["cost"] = cr.Usage.Cost
	}
	extra := map[string]any{"usage": usage}
	for key, raw := range map[string]json.RawMessage{"model": cr.Model, "id": cr.ID, "provider": cr.Provider} {
		if len(raw) > 0 {
			extra[key] = raw
		}
	}
	body, err := AnswersToSystemOne(strings.TrimSpace(*cr.Choices[0].Message.Content), questions, extra)
	if err != nil {
		return Response{}, err
	}
	res, err := systemone.DecodeTrial(systemone.Request{Questions: questions}, body)
	if err != nil {
		return Response{}, err
	}
	resp := mapResult(res)
	resp.Body = string(respBody)
	return resp, nil
}

// intOf is raw as a count (0 when it is not a non-negative integer).
func intOf(raw json.RawMessage) int {
	var n int
	if json.Unmarshal(raw, &n) != nil || n < 0 {
		return 0
	}
	return n
}
