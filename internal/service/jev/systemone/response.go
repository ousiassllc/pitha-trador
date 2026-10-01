package systemone

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
)

// ErrInvalidResponse marks a 200 response that is not a valid answer set
// for the questions asked (malformed JSON, missing or mistyped answer,
// choice outside the defined options, out-of-range probability). Retrying
// the same request does not fix it.
var ErrInvalidResponse = errors.New("jev: invalid response")

// Usage is the token usage the API reports for a request.
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// answer is one raw answer as sent on the wire. Pointers distinguish a
// missing field from a zero value.
type answer struct {
	Type       string   `json:"type"`
	Noul       *float64 `json:"noul"`
	Choice     *string  `json:"choice"`
	Confidence *float64 `json:"confidence"`
}

type response struct {
	Model   string            `json:"model"`
	Answers map[string]answer `json:"answers"`
	Usage   Usage             `json:"usage"`
}

// Choice is a validated choice answer.
type Choice struct {
	// Value is the highest-probability option, one of the question's
	// defined options.
	Value string
	// Confidence is the API's certainty in [0, 1] derived from the
	// option probabilities. It is Jev's self-reported certainty, not a
	// verified probability (FR-TRADER-2).
	Confidence float64
}

// Result is a response that Decode validated against its request: every
// asked question has an answer of the asked type with an in-range value.
type Result struct {
	// Model is the model that performed the evaluation, e.g. "jev-1.13.0".
	Model string
	Usage Usage

	noul    map[string]float64
	choices map[string]Choice
}

// Noul returns the yes-probability in [0, 1] of the noul question id
// (0 if id was not a noul question of the request).
func (r Result) Noul(id string) float64 { return r.noul[id] }

// Choice returns the validated answer of the choice question id (the
// zero Choice if id was not a choice question of the request).
func (r Result) Choice(id string) Choice { return r.choices[id] }

// Decode parses body as the response to req and validates every answer
// against the question it answers. Any violation returns an error that
// wraps ErrInvalidResponse. Answers for ids that were not asked are
// ignored.
func Decode(req Request, body []byte) (Result, error) {
	var resp response
	if err := json.Unmarshal(body, &resp); err != nil {
		return Result{}, fmt.Errorf("%w: decode body: %w", ErrInvalidResponse, err)
	}
	if resp.Model == "" {
		return Result{}, fmt.Errorf("%w: missing model", ErrInvalidResponse)
	}

	result := Result{
		Model:   resp.Model,
		Usage:   resp.Usage,
		noul:    make(map[string]float64),
		choices: make(map[string]Choice),
	}
	ids := make([]string, 0, len(req.Questions))
	for id := range req.Questions {
		ids = append(ids, id)
	}
	slices.Sort(ids)

	for _, id := range ids {
		q := req.Questions[id]
		a, ok := resp.Answers[id]
		if !ok {
			return Result{}, fmt.Errorf("%w: missing answer %q", ErrInvalidResponse, id)
		}
		if a.Type != q.Type {
			return Result{}, fmt.Errorf("%w: answer %q has type %q, want %q", ErrInvalidResponse, id, a.Type, q.Type)
		}
		switch q.Type {
		case TypeNoul:
			if a.Noul == nil || !inUnitRange(*a.Noul) {
				return Result{}, fmt.Errorf("%w: answer %q noul is missing or outside [0, 1]", ErrInvalidResponse, id)
			}
			result.noul[id] = *a.Noul
		case TypeChoice:
			c, err := validateChoice(id, q, a)
			if err != nil {
				return Result{}, err
			}
			result.choices[id] = c
		default:
			return Result{}, fmt.Errorf("%w: question %q has unsupported type %q", ErrInvalidResponse, id, q.Type)
		}
	}
	return result, nil
}

func validateChoice(id string, q Question, a answer) (Choice, error) {
	options, _ := q.Criteria.(Options)
	if a.Choice == nil || !slices.ContainsFunc(options, func(o Option) bool { return o.Name == *a.Choice }) {
		got := "<missing>"
		if a.Choice != nil {
			got = *a.Choice
		}
		return Choice{}, fmt.Errorf("%w: answer %q choice %q is not a defined option", ErrInvalidResponse, id, got)
	}
	if a.Confidence == nil || !inUnitRange(*a.Confidence) {
		return Choice{}, fmt.Errorf("%w: answer %q confidence is missing or outside [0, 1]", ErrInvalidResponse, id)
	}
	return Choice{Value: *a.Choice, Confidence: *a.Confidence}, nil
}

func inUnitRange(v float64) bool { return v >= 0 && v <= 1 }
