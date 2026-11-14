package trialwire

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/jev"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/systemone"
	"github.com/ousiassllc/pitha-trador/internal/service/rag"
)

// Question ids of the FR-TRADER-1 group (the live ones, jev.questions_trader.go;
// a test pins that every registered version asks exactly these).
const (
	qDirection               = "direction"
	qRegime                  = "regime"
	qEntryQuality            = "entry_quality"
	qToxicFlow               = "toxic_flow"
	qLiquidityStressed       = "liquidity_stressed"
	qContinuationProbability = "continuation_probability"
)

// state is the "state" of the request: the same {"market", "similar_past_cases"}
// shape the live Trader sends (jev.systemOneState; a test compares the two
// request bodies).
type state struct {
	Market           jev.ScoutState `json:"market"`
	SimilarPastCases rag.Context    `json:"similar_past_cases"`
}

// Response is a trial model's answer to the FR-TRADER-1 question group, read
// with the trial's looser validation: an answer the provider refused is ""
// or nil and listed in Refused, a choice without confidence has a nil
// Confidence. It is never a jev.TraderResponse: nothing of it reaches the live
// Trader, Policy, the RAG or the calibration.
type Response struct {
	// Direction, Regime and EntryQuality are the choice answers ("" when refused).
	Direction    string
	Regime       string
	EntryQuality string
	// ToxicFlow, LiquidityStressed and ContinuationProbability are the noul
	// answers (nil when refused).
	ToxicFlow               *float64
	LiquidityStressed       *float64
	ContinuationProbability *float64
	// Confidence is the direction answer's confidence, nil when the model
	// reported none or refused the question.
	Confidence *float64
	// DirectionProbabilities are the direction option probabilities as received.
	DirectionProbabilities map[string]float64
	// Refused lists the refused question ids, sorted.
	Refused []string

	// ModelID is the response's "model" (what served the call); Provider and
	// ResponseID are its "provider" and "id" ("" when absent).
	ModelID    string
	Provider   string
	ResponseID string
	// InputTokens and OutputTokens are usage (0 when the response carried
	// none). ReportedCostUSD is usage.cost, nil when not reported: the caller
	// estimates the cost then, it is never 0 USD.
	InputTokens     int
	OutputTokens    int
	ReportedCostUSD *float64
	// Body is the raw response body.
	Body string
}

// Trader asks the FR-TRADER-1 question group of req.QuestionVersion (empty:
// the build default; an unregistered version is an error and nothing is sent)
// about req's state. It returns the answers, the call's latency and the size of
// the request body (set on failure too, once the body was built).
func (c *Client) Trader(ctx context.Context, req jev.TraderRequest) (Response, time.Duration, int, error) {
	questions, err := jev.TraderQuestionsFor(req.QuestionVersion)
	if err != nil {
		return Response{}, 0, 0, err
	}
	start := time.Now()
	res, requestBytes, err := c.do(ctx, buildRequest(c.model, c.provider, questions, req))
	latency := time.Since(start)
	if err != nil {
		return Response{}, latency, requestBytes, err
	}
	return mapResult(res), latency, requestBytes, nil
}

func buildRequest(model string, provider *systemone.Provider, questions map[string]systemone.Question, req jev.TraderRequest) systemone.Request {
	return systemone.Request{
		State:     state{Market: req.State, SimilarPastCases: req.RAGContext},
		Model:     model,
		Questions: questions,
		Provider:  provider,
	}
}

// RequestBytes is the size of the request body Trader would send for req to an
// arm with model and provider, without sending anything. The trial uses it to
// reserve a call's worst-case cost before the call is made.
func RequestBytes(model string, provider *systemone.Provider, req jev.TraderRequest) (int, error) {
	questions, err := jev.TraderQuestionsFor(req.QuestionVersion)
	if err != nil {
		return 0, err
	}
	body, err := json.Marshal(buildRequest(model, provider, questions, req))
	if err != nil {
		return 0, fmt.Errorf("trialwire: encode request: %w", err)
	}
	return len(body), nil
}

func mapResult(res systemone.Result) Response {
	resp := Response{
		ModelID:         res.Model,
		Provider:        res.Provider,
		ResponseID:      res.ID,
		InputTokens:     res.Usage.InputTokens,
		OutputTokens:    res.Usage.OutputTokens,
		ReportedCostUSD: res.ReportedCostUSD,
		Body:            res.Body,
	}
	for _, id := range []string{qDirection, qRegime, qEntryQuality, qToxicFlow, qLiquidityStressed, qContinuationProbability} {
		if res.Refused(id) {
			resp.Refused = append(resp.Refused, id)
		}
	}
	slices.Sort(resp.Refused)
	if !res.Refused(qDirection) {
		direction := res.Choice(qDirection)
		resp.Direction, resp.DirectionProbabilities = direction.Value, direction.Probabilities
		if direction.ConfidenceReported {
			confidence := direction.Confidence
			resp.Confidence = &confidence
		}
	}
	if !res.Refused(qRegime) {
		resp.Regime = res.Choice(qRegime).Value
	}
	if !res.Refused(qEntryQuality) {
		resp.EntryQuality = res.Choice(qEntryQuality).Value
	}
	resp.ToxicFlow = noul(res, qToxicFlow)
	resp.LiquidityStressed = noul(res, qLiquidityStressed)
	resp.ContinuationProbability = noul(res, qContinuationProbability)
	return resp
}

func noul(res systemone.Result, id string) *float64 {
	if res.Refused(id) {
		return nil
	}
	v := res.Noul(id)
	return &v
}
