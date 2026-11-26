package trialwire

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/ousiassllc/pitha-trador/internal/service/jev/systemone"
)

// The FR-TRADER-1 question group has no System One endpoint on a model that is not
// a decision model: it is written into a prompt, answered as a JSON object keyed
// by question id (AnswerSchema), and that object is rewritten as a System One
// answer set (AnswersToSystemOne) so systemone.DecodeTrial reads it. The Codex
// CLI (codexwire, protocol codex-exec-v1) and the chat Completions arm (this
// package, ChatProtocolVersion) share these three pieces, so the two wires ask the
// same questions with the same schema and read the answer the same way; they
// differ only in the instruction around them and in how the call is made. A change
// of any of them changes both protocols' requests: bump their versions.

// QuestionsPrompt is the prompt text of the questions: each id in brackets (the
// answer key), its instructions, and, for a choice, its options and their rubrics
// or, for a noul question, what a yes and a no mean. An unsupported kind of question
// is an error.
func QuestionsPrompt(questions map[string]systemone.Question) (string, error) {
	var b strings.Builder
	b.WriteString("Questions (the answer key is the id in brackets):\n")
	for _, id := range sortedIDs(questions) {
		q := questions[id]
		fmt.Fprintf(&b, "\n[%s] %s\n", id, q.Instructions)
		switch c := q.Criteria.(type) {
		case systemone.NoulCriteria:
			fmt.Fprintf(&b, "  Answer a probability from 0 to 1. Near 1 means: %s. Near 0 means: %s.\n", c.True, c.False)
		case systemone.Options:
			b.WriteString("  Answer with one option and your confidence (0 to 1). The options:\n")
			for _, o := range c {
				fmt.Fprintf(&b, "  - %s: %s\n", o.Name, o.Rubric)
			}
		default:
			return "", fmt.Errorf("trialwire: question %q has unsupported criteria", id)
		}
	}
	return b.String(), nil
}

// StatePrompt is the prompt text of the state: its JSON.
func StatePrompt(state any) (string, error) {
	stateJSON, err := json.Marshal(state)
	if err != nil {
		return "", fmt.Errorf("trialwire: encode state: %w", err)
	}
	return "State (JSON):\n" + string(stateJSON) + "\n", nil
}

func sortedIDs(questions map[string]systemone.Question) []string {
	ids := make([]string, 0, len(questions))
	for id := range questions {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

// AnswerSchema is the JSON Schema of the answer object: every question is a
// required key, and null is the answer of a question the model declines. A choice is
// {"choice", "confidence"}, a noul question a number from 0 to 1. It keeps to the
// subset OpenAI's structured outputs accept (all keys required,
// additionalProperties false, anyOf with null, enum, minimum/maximum).
func AnswerSchema(questions map[string]systemone.Question) ([]byte, error) {
	props := map[string]any{}
	for _, id := range sortedIDs(questions) {
		q := questions[id]
		var shape any
		switch c := q.Criteria.(type) {
		case systemone.NoulCriteria:
			shape = map[string]any{"type": "number", "minimum": 0, "maximum": 1}
		case systemone.Options:
			names := make([]string, len(c))
			for i, o := range c {
				names[i] = o.Name
			}
			shape = map[string]any{
				"type": "object", "additionalProperties": false, "required": []string{"choice", "confidence"},
				"properties": map[string]any{
					"choice":     map[string]any{"type": "string", "enum": names},
					"confidence": map[string]any{"type": "number", "minimum": 0, "maximum": 1},
				},
			}
		default:
			return nil, fmt.Errorf("trialwire: question %q has unsupported criteria", id)
		}
		props[id] = map[string]any{"anyOf": []any{shape, map[string]any{"type": "null"}}}
	}
	return json.Marshal(map[string]any{
		"type": "object", "additionalProperties": false, "required": sortedIDs(questions), "properties": props,
	})
}

// AnswersToSystemOne rewrites the model's answers (a JSON object keyed by question
// id, see AnswerSchema) as the body of a System One response, merging extra (the
// response's "model", "usage", "id", "provider"; whatever the caller read from its
// own envelope) into it. A message that is not a JSON object, a question without an
// answer, an answer that was not asked, and an answer of the wrong shape are
// systemone.ErrInvalidResponse; whether a value is in range or a choice is one of the
// options is left to systemone.DecodeTrial. A null answer is a refusal.
func AnswersToSystemOne(message string, questions map[string]systemone.Question, extra map[string]any) ([]byte, error) {
	var given map[string]json.RawMessage
	dec := json.NewDecoder(strings.NewReader(message))
	if err := dec.Decode(&given); err != nil {
		return nil, fmt.Errorf("%w: the answers are not a JSON object: %w", systemone.ErrInvalidResponse, err)
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
	body := map[string]any{"answers": answers}
	for k, v := range extra {
		body[k] = v
	}
	return json.Marshal(body)
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
