package codexwire

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/ousiassllc/pitha-trador/internal/service/jev/systemone"
)

// protocolVersion names the way the question group is put to Codex (the prompt
// layout and the output schema below). Codex has no System One endpoint: the
// same questions are written into one prompt and answered through
// --output-schema, so a Codex arm differs from an OpenRouter arm in the protocol
// as well as in the model. It is part of the request, so a change of the layout
// must change it.
const protocolVersion = "codex-exec-v1"

// preamble is the fixed instruction of every prompt. Codex is an agent: the
// call asks it to use no tool and to answer with the JSON object only.
const preamble = "You evaluate one market state for a trading research comparison. " +
	"Use only the state below. Do not use any tool, do not run any command, do not read or write any file, do not browse. " +
	"Answer every question once and reply with a single JSON object that matches the given output schema and nothing else. " +
	"If you cannot or will not answer a question, answer null for it."

// buildPrompt is the prompt for questions about state (any JSON-encodable value).
func buildPrompt(questions map[string]systemone.Question, state any) (string, error) {
	stateJSON, err := json.Marshal(state)
	if err != nil {
		return "", fmt.Errorf("codexwire: encode state: %w", err)
	}
	var b strings.Builder
	b.WriteString(preamble)
	b.WriteString("\n\nQuestions (the answer key is the id in brackets):\n")
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
			return "", fmt.Errorf("codexwire: question %q has unsupported criteria", id)
		}
	}
	b.WriteString("\nState (JSON):\n")
	b.Write(stateJSON)
	b.WriteString("\n")
	return b.String(), nil
}

func sortedIDs(questions map[string]systemone.Question) []string {
	ids := make([]string, 0, len(questions))
	for id := range questions {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

// schemaFor is the JSON Schema of the answer object: every question is a
// required key, and null is the answer of a question the model declines. It keeps
// to the subset OpenAI's structured outputs accept (all keys required,
// additionalProperties false, anyOf with null, enum, minimum/maximum).
func schemaFor(questions map[string]systemone.Question) ([]byte, error) {
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
			return nil, fmt.Errorf("codexwire: question %q has unsupported criteria", id)
		}
		props[id] = map[string]any{"anyOf": []any{shape, map[string]any{"type": "null"}}}
	}
	return json.Marshal(map[string]any{
		"type": "object", "additionalProperties": false, "required": sortedIDs(questions), "properties": props,
	})
}
