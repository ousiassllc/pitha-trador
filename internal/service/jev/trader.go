package jev

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository"
	"github.com/ousiassllc/pitha-trador/internal/service/rag"
)

// Trader evaluates Jev Scout-passed candidates against Jev's FR-TRADER-1
// question group and persists every completed call to jev_decisions
// (decision_type=trader, FR-TRADER-3).
//
// FR-TRADER-2: TraderResponse.Confidence (and the yes-probability fields
// ToxicFlow/LiquidityStressed/ContinuationProbability) are Jev's own
// self-reported confidence, never a verified probability of an actual
// price move. Trader does not threshold, filter, or otherwise act on
// these values - it only records them. Turning them into a trading
// decision is Policy Engine's job (a later sub-scope, functional.md
// §4.6), and checking how well they track real outcomes is
// Calibration's job (functional.md §4.9). Keeping that judgment out of
// this type is what keeps FR-TRADER-2 true as the codebase grows.
type Trader struct {
	client    *Client
	decisions *repository.DecisionRepository
	rag       *rag.Service
}

// NewTrader returns a Trader that calls client, persists decisions via
// decisions, and builds/indexes the RAG few-shot context via ragService
// - the same roles Scout's fields play for Jev Scout (functional.md
// §4.13, FR-RAG-1〜4).
func NewTrader(client *Client, decisions *repository.DecisionRepository, ragService *rag.Service) *Trader {
	return &Trader{client: client, decisions: decisions, rag: ragService}
}

// Evaluate builds the RAG few-shot context for state (FR-RAG-2, FR-RAG-3),
// calls Jev Trader for one Scout-passed instrument's current state,
// persists the resulting jev_decisions row (FR-TRADER-3) and its
// standardized feature embedding (FR-RAG-1), and returns the decision -
// direction/regime/entry_quality/confidence/toxic_flow/
// liquidity_stressed/continuation_probability included - for a caller
// (e.g. a later Policy Engine sub-scope) to act on. A Jev API failure
// (after the Client's retry policy is exhausted) returns an error and
// persists nothing: overview.md §6 "継続失敗でnew entry停止". RAG context
// search/indexing failures are logged, not returned: a technical fault
// in the RAG side must never block Jev from being called (functional.md
// FR-RAG-4's cold-start tolerance extends to any RAG failure, not only
// an empty index).
func (t *Trader) Evaluate(ctx context.Context, instrumentID int64, state ScoutState) (domain.JevDecision, error) {
	featureInput := ragFeatureInput(state)

	ragContext, err := t.rag.Context(ctx, featureInput, rag.DefaultK)
	if err != nil {
		slog.Error("jev: build rag context failed, calling Trader without similar-case context", "symbol", state.Symbol, "error", err)
	}

	stateJSON, err := json.Marshal(state)
	if err != nil {
		return domain.JevDecision{}, fmt.Errorf("jev: encode trader state for %q: %w", state.Symbol, err)
	}

	resp, latency, err := t.client.Trader(ctx, TraderRequest{QuestionVersion: TraderQuestionVersion, State: state, RAGContext: ragContext})
	if err != nil {
		return domain.JevDecision{}, fmt.Errorf("jev: trader evaluation for %q: %w", state.Symbol, err)
	}

	responseJSON, err := json.Marshal(resp)
	if err != nil {
		return domain.JevDecision{}, fmt.Errorf("jev: encode trader response for %q: %w", state.Symbol, err)
	}

	direction := resp.Direction
	regime := resp.Regime
	entryQuality := resp.EntryQuality
	confidence := resp.Confidence
	toxicFlow := resp.ToxicFlow
	liquidityStressed := resp.LiquidityStressed
	continuationProbability := resp.ContinuationProbability

	decision := domain.JevDecision{
		InstrumentID:            instrumentID,
		Symbol:                  state.Symbol,
		Timestamp:               state.Timestamp,
		DecisionType:            domain.JevDecisionTypeTrader,
		StateHash:               hashState(stateJSON),
		StateJSON:               string(stateJSON),
		QuestionVersion:         TraderQuestionVersion,
		ResponseJSON:            string(responseJSON),
		Direction:               &direction,
		Regime:                  &regime,
		EntryQuality:            &entryQuality,
		Confidence:              &confidence,
		ToxicFlow:               &toxicFlow,
		LiquidityStressed:       &liquidityStressed,
		ContinuationProbability: &continuationProbability,
		LatencyMs:               int(latency.Milliseconds()),
		ModelID:                 resp.ModelID,
		RequestCost:             resp.RequestCost,
	}

	saved, err := t.decisions.Insert(ctx, decision)
	if err != nil {
		return domain.JevDecision{}, fmt.Errorf("jev: persist trader decision for %q: %w", state.Symbol, err)
	}

	if err := t.rag.IndexDecision(ctx, saved.ID, featureInput); err != nil {
		slog.Error("jev: index decision vector failed", "decision_id", saved.ID, "symbol", state.Symbol, "error", err)
	}

	return saved, nil
}
