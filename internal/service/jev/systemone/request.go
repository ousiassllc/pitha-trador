// Package systemone is the wire layer of the TypeSafe AI evaluation API
// (POST /v1/systemone, https://docs.typesafe.ai/api): the typed
// question request, the typed answer response and the strict validation
// of an answer set against the questions it answers. It knows nothing
// about Scout/Trader; internal/service/jev builds the questions and maps
// the validated Result onto its domain-level responses.
package systemone

import (
	"bytes"
	"encoding/json"
)

// Question types of the API. Only noul and choice are used by this
// application; score is deliberately not modelled.
const (
	TypeNoul   = "noul"
	TypeChoice = "choice"
)

// Request is the POST body of the evaluation endpoint.
type Request struct {
	// State is the content to evaluate: any JSON-encodable value.
	State any `json:"state"`
	// Model is the model alias, e.g. "jev-latest".
	Model string `json:"model"`
	// Questions maps a question id (chosen by the caller, not sent to
	// the model) to the typed question. Answers come back under the
	// same ids.
	Questions map[string]Question `json:"questions"`
}

// Question is one typed question. Build it with NoulQuestion or ChoiceQuestion.
type Question struct {
	Type         string `json:"type"`
	Instructions string `json:"instructions"`
	// Criteria is NoulCriteria for a noul question and Options for a
	// choice question.
	Criteria any `json:"criteria,omitempty"`
}

// NoulCriteria describes what a yes (value near 1) and a no (value near
// 0) mean for a noul question.
type NoulCriteria struct {
	True  string `json:"true"`
	False string `json:"false"`
}

// Option is one selectable value of a choice question together with the
// rubric that tells the model when to pick it.
type Option struct {
	Name   string
	Rubric string
}

// Options is the ordered option set of a choice question. It encodes as
// the API's criteria object (option -> rubric) in declaration order, so
// ordinal scales such as weak..exceptional keep their order on the wire.
type Options []Option

// MarshalJSON encodes o as a JSON object whose keys keep o's order.
func (o Options) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, opt := range o {
		if i > 0 {
			buf.WriteByte(',')
		}
		name, err := json.Marshal(opt.Name)
		if err != nil {
			return nil, err
		}
		rubric, err := json.Marshal(opt.Rubric)
		if err != nil {
			return nil, err
		}
		buf.Write(name)
		buf.WriteByte(':')
		buf.Write(rubric)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// NoulQuestion returns a yes/no question answered with the probability of yes.
func NoulQuestion(instructions, yes, no string) Question {
	return Question{Type: TypeNoul, Instructions: instructions, Criteria: NoulCriteria{True: yes, False: no}}
}

// ChoiceQuestion returns a question answered with exactly one of options.
func ChoiceQuestion(instructions string, options ...Option) Question {
	return Question{Type: TypeChoice, Instructions: instructions, Criteria: Options(options)}
}
