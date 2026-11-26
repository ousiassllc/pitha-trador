package trialwire

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/jev"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/systemone"
	"github.com/ousiassllc/pitha-trador/internal/service/rag"
)

// rawState is the "state" of a replayed request (issue #1085): the same
// {"market", "similar_past_cases"} shape as state, but "market" is the saved
// input (jev_decisions.state_json) as stored, never decoded and re-encoded, so
// the arm is sent exactly what the live Jev was sent and no field is added or
// dropped. similar_past_cases is always empty: a RAG context built now would
// carry decisions newer than the replayed one.
type rawState struct {
	Market           json.RawMessage `json:"market"`
	SimilarPastCases rag.Context     `json:"similar_past_cases"`
}

// TraderRaw is Trader for a saved input: it asks the question group of
// questionVersion about stateJSON (a stored jev_decisions.state_json, sent as
// is) with an empty similar-case context. Like Trader it returns the answers,
// the call's latency and the request body's size (set on failure too, once the
// body was built); a stateJSON that is not valid JSON, or an unregistered
// questionVersion, is an error and nothing is sent.
func (c *Client) TraderRaw(ctx context.Context, questionVersion string, stateJSON json.RawMessage) (Response, time.Duration, int, error) {
	questions, err := jev.TraderQuestionsFor(questionVersion)
	if err != nil {
		return Response{}, 0, 0, err
	}
	start := time.Now()
	resp, requestBytes, err := c.ask(ctx, questions, rawState{Market: stateJSON})
	return resp, time.Since(start), requestBytes, err
}

// RawRequestBytes is the size of the request body TraderRaw would send to an
// arm with model and provider, without sending anything (a replay reserves a
// call's worst-case cost from it before the call is made).
func RawRequestBytes(model string, provider *systemone.Provider, questionVersion string, stateJSON json.RawMessage) (int, error) {
	questions, err := jev.TraderQuestionsFor(questionVersion)
	if err != nil {
		return 0, err
	}
	body, err := json.Marshal(buildRequest(model, provider, questions, rawState{Market: stateJSON}))
	if err != nil {
		return 0, fmt.Errorf("trialwire: encode request: %w", err)
	}
	return len(body), nil
}
