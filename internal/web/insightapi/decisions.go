package insightapi

import (
	"context"
	"errors"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/ousiassllc/pitha-trador/internal/service/execution"
)

// decisionOutput mirrors one `GET /api/v1/symbols/{symbol}/decisions`
// item: a jev_decisions row without its raw state/response JSON.
// Direction/Regime/EntryQuality/Confidence/ToxicFlow/LiquidityStressed/
// ContinuationProbability are set only for decision_type "trader".
type decisionOutput struct {
	ID                      int64    `json:"id"`
	Symbol                  string   `json:"symbol"`
	Timestamp               string   `json:"timestamp"`
	DecisionType            string   `json:"decision_type" enum:"scout,trader"`
	Direction               *string  `json:"direction"`
	Confidence              *float64 `json:"confidence"`
	Regime                  *string  `json:"regime"`
	EntryQuality            *string  `json:"entry_quality"`
	ToxicFlow               *float64 `json:"toxic_flow"`
	LiquidityStressed       *float64 `json:"liquidity_stressed"`
	ContinuationProbability *float64 `json:"continuation_probability"`
	QuestionVersion         string   `json:"question_version"`
	ModelID                 string   `json:"model_id"`
	LatencyMs               int      `json:"latency_ms"`
}

// DecisionsAPIOutput is the Huma response body for `GET
// /api/v1/symbols/{symbol}/decisions`.
type DecisionsAPIOutput struct {
	Body struct {
		Symbol string           `json:"symbol"`
		Items  []decisionOutput `json:"items"`
	}
}

// DecisionsInput is `GET /api/v1/symbols/{symbol}/decisions`'s
// path+query parameters.
type DecisionsInput struct {
	Symbol string `path:"symbol" minLength:"1" maxLength:"16" pattern:"^[0-9A-Za-z]+$" doc:"Instrument symbol (alphanumeric, e.g. 7203)."`
	Limit  int    `query:"limit" default:"100" minimum:"1" maximum:"500" doc:"Maximum number of decisions to return (1-500)."`
}

// Decisions implements `GET /api/v1/symbols/{symbol}/decisions`
// (docs/api/endpoints.md §5): the symbol's jev_decisions history, Scout
// and Trader decisions interleaved and distinguished by decision_type,
// most recent first.
func (h *Handler) Decisions(ctx context.Context, in *DecisionsInput) (*DecisionsAPIOutput, error) {
	decisions, err := h.provider.RecentDecisions(ctx, in.Symbol, in.Limit)
	if err != nil {
		if errors.Is(err, execution.ErrInstrumentUnknown) {
			return nil, huma.Error404NotFound("unknown symbol")
		}
		return nil, huma.Error500InternalServerError("list decisions failed", err)
	}

	out := &DecisionsAPIOutput{}
	out.Body.Symbol = in.Symbol
	out.Body.Items = make([]decisionOutput, len(decisions))
	for i, d := range decisions {
		out.Body.Items[i] = decisionOutput{
			ID: d.ID, Symbol: d.Symbol, Timestamp: d.Timestamp.Format(time.RFC3339),
			DecisionType: d.DecisionType, Direction: d.Direction, Confidence: d.Confidence,
			Regime: d.Regime, EntryQuality: d.EntryQuality, ToxicFlow: d.ToxicFlow,
			LiquidityStressed: d.LiquidityStressed, ContinuationProbability: d.ContinuationProbability,
			QuestionVersion: d.QuestionVersion, ModelID: d.ModelID, LatencyMs: d.LatencyMs,
		}
	}
	return out, nil
}
