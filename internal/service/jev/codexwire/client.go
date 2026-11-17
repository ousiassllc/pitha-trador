package codexwire

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/jev"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/systemone"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/trialwire"
)

// ServedProvider is the "provider" a Codex answer is stored with (served_provider):
// the CLI's output names none, so it is the provider with the protocol the question
// group was put in (protocolVersion), which a comparison must keep apart from an
// OpenRouter arm's System One request.
const ServedProvider = "codex_chatgpt:" + protocolVersion

// Client calls one trial arm's model through the Codex CLI. It runs one process
// per call; the caller (the trial) makes sure only one call runs at a time.
type Client struct {
	cfg   Config
	login *Login
	now   func() time.Time
}

// New returns a Client for cfg. login is the shared login check of the dedicated
// CODEX_HOME (NewLogin).
func New(cfg Config, login *Login) *Client {
	return &Client{cfg: cfg, login: login, now: time.Now}
}

// Configured reports whether a dedicated CODEX_HOME is set: without one the
// person's own Codex home would be used, which is never done. Whether the CLI is
// logged in is Login's question.
func (c *Client) Configured() bool { return c != nil && c.cfg.Home != "" }

// Login reports the state of the CLI's login (one of the
// jevtrialsettings.CodexLogin* states) and when it was checked. The state is
// remembered for a short while, so asking before every call costs no process.
func (c *Client) Login(ctx context.Context) (state string, checkedAt time.Time) {
	if c == nil || c.login == nil {
		return "", time.Time{}
	}
	return c.login.State(ctx)
}

// Trader asks the FR-TRADER-1 question group of req.QuestionVersion about
// req's state with one `codex exec` run. It returns the answers, the call's latency
// and the size of the prompt. The error is a *CallError when the run failed, wraps
// systemone.ErrInvalidResponse when it ran but its answer is not a valid answer
// set, and is otherwise a build error (nothing was run).
func (c *Client) Trader(ctx context.Context, req jev.TraderRequest) (trialwire.Response, time.Duration, int, error) {
	questions, err := jev.TraderQuestionsFor(req.QuestionVersion)
	if err != nil {
		return trialwire.Response{}, 0, 0, err
	}
	prompt, err := buildPrompt(questions, trialwire.StateOf(req))
	if err != nil {
		return trialwire.Response{}, 0, 0, err
	}
	schema, err := schemaFor(questions)
	if err != nil {
		return trialwire.Response{}, 0, len(prompt), err
	}
	start := time.Now()
	out, err := c.runExec(ctx, prompt, schema)
	latency := time.Since(start)
	if err != nil {
		return trialwire.Response{}, latency, len(prompt), err
	}
	resp, err := c.answer(questions, out)
	return resp, latency, len(prompt), err
}

// RequestBytes is the size of the prompt Trader would send for req.
func RequestBytes(req jev.TraderRequest) (int, error) {
	questions, err := jev.TraderQuestionsFor(req.QuestionVersion)
	if err != nil {
		return 0, err
	}
	prompt, err := buildPrompt(questions, trialwire.StateOf(req))
	return len(prompt), err
}

// answer turns the model's final message into a trialwire.Response. The message
// is a JSON object keyed by question id (see schemaFor); it is rewritten as a
// System One answer set and read with systemone.DecodeTrial, so it is validated
// exactly as strictly as an OpenRouter answer (a missing question, a value out of
// range or a choice that is not an option is systemone.ErrInvalidResponse).
func (c *Client) answer(questions map[string]systemone.Question, out outcome) (trialwire.Response, error) {
	body, err := systemOneBody(c.cfg.Model, questions, out)
	if err != nil {
		return trialwire.Response{}, err
	}
	res, err := systemone.DecodeTrial(systemone.Request{Questions: questions}, body)
	if err != nil {
		return trialwire.Response{}, err
	}
	resp := trialwire.FromResult(res)
	resp.Provider, resp.ResponseID = ServedProvider, out.threadID
	resp.InputTokens, resp.OutputTokens = out.usage.InputTokens, out.usage.OutputTokens
	resp.ReportedCostUSD = nil // a subscription reports no USD
	resp.Body = out.message
	return resp, nil
}

// systemOneBody rewrites the model's answers (out.message) as the body of a
// System One response.
func systemOneBody(model string, questions map[string]systemone.Question, out outcome) ([]byte, error) {
	var given map[string]json.RawMessage
	dec := json.NewDecoder(strings.NewReader(out.message))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&given); err != nil {
		return nil, fmt.Errorf("%w: the final message is not a JSON object: %w", systemone.ErrInvalidResponse, err)
	}
	answers := map[string]any{}
	for id, q := range questions {
		raw, ok := given[id]
		if !ok {
			return nil, fmt.Errorf("%w: missing answer %q", systemone.ErrInvalidResponse, id)
		}
		a, err := oneAnswer(id, q, raw)
		if err != nil {
			return nil, err
		}
		answers[id] = a
	}
	for id := range given {
		if _, ok := questions[id]; !ok {
			return nil, fmt.Errorf("%w: answer %q was not asked", systemone.ErrInvalidResponse, id)
		}
	}
	return json.Marshal(map[string]any{
		"model":   model,
		"answers": answers,
		"usage":   map[string]int{"input_tokens": out.usage.InputTokens, "output_tokens": out.usage.OutputTokens},
	})
}

// oneAnswer is the System One answer of raw (null: the model declined).
func oneAnswer(id string, q systemone.Question, raw json.RawMessage) (map[string]any, error) {
	if string(raw) == "null" {
		return map[string]any{"type": systemone.TypeRefusal}, nil
	}
	switch q.Type {
	case systemone.TypeNoul:
		var v *float64
		if err := json.Unmarshal(raw, &v); err != nil || v == nil {
			return nil, fmt.Errorf("%w: answer %q is not a number", systemone.ErrInvalidResponse, id)
		}
		return map[string]any{"type": systemone.TypeNoul, "noul": *v}, nil
	case systemone.TypeChoice:
		var v struct {
			Choice     *string  `json:"choice"`
			Confidence *float64 `json:"confidence"`
		}
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, fmt.Errorf("%w: answer %q is not a choice object", systemone.ErrInvalidResponse, id)
		}
		return map[string]any{"type": systemone.TypeChoice, "choice": v.Choice, "confidence": v.Confidence}, nil
	}
	return nil, fmt.Errorf("%w: question %q has unsupported type %q", systemone.ErrInvalidResponse, id, q.Type)
}
