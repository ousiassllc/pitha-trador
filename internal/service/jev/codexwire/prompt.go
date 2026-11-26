package codexwire

import (
	"github.com/ousiassllc/pitha-trador/internal/service/jev/systemone"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/trialwire"
)

// protocolVersion names the way the question group is put to Codex (the prompt
// layout and the output schema below). Codex has no System One endpoint: the
// same questions are written into one prompt and answered through
// --output-schema, so a Codex arm differs from an OpenRouter arm in the protocol
// as well as in the model. It is part of the request, so a change of the layout
// must change it. The questions, the state and the schema are the ones the chat
// Completions arm uses (trialwire.QuestionsPrompt, StatePrompt, AnswerSchema); a
// test in this package pins that Codex's bytes did not move when they were shared.
const protocolVersion = "codex-exec-v1"

// preamble is the fixed instruction of every prompt. Codex is an agent: the
// call asks it to use no tool and to answer with the JSON object only.
const preamble = "You evaluate one market state for a trading research comparison. " +
	"Use only the state below. Do not use any tool, do not run any command, do not read or write any file, do not browse. " +
	"Answer every question once and reply with a single JSON object that matches the given output schema and nothing else. " +
	"If you cannot or will not answer a question, answer null for it."

// buildPrompt is the prompt for questions about state (any JSON-encodable value).
func buildPrompt(questions map[string]systemone.Question, state any) (string, error) {
	q, err := trialwire.QuestionsPrompt(questions)
	if err != nil {
		return "", err
	}
	s, err := trialwire.StatePrompt(state)
	if err != nil {
		return "", err
	}
	return preamble + "\n\n" + q + "\n" + s, nil
}

// schemaFor is the JSON Schema of the answer object (trialwire.AnswerSchema).
func schemaFor(questions map[string]systemone.Question) ([]byte, error) {
	return trialwire.AnswerSchema(questions)
}
