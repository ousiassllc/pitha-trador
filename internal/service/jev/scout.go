package jev

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/jobqueue"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/service/rag"
)

// Passes reports whether resp clears every FR-SCOUT-2 pass-condition
// threshold in cfg (functional.md §4.4: "interesting_now >= 0.65 AND
// liquidity_ok >= 0.70 AND abnormal_activity >= 0.55", initial values -
// all three are required (AND), and cfg makes them configurable).
func Passes(cfg config.JevScoutConfig, resp ScoutResponse) bool {
	return resp.InterestingNow >= cfg.MinInterestingNow &&
		resp.LiquidityOk >= cfg.MinLiquidityOk &&
		resp.AbnormalActivity >= cfg.MinAbnormalActivity
}

// StateFromSnapshot builds the ScoutState Jev evaluates from one
// instrument's market_snapshots row (domain.Snapshot).
func StateFromSnapshot(snap domain.Snapshot) ScoutState {
	return ScoutState{
		Symbol:             snap.Symbol,
		Timestamp:          snap.Timestamp,
		Price:              snap.Price,
		Return1m:           snap.Feature.Return1m,
		Return5m:           snap.Feature.Return5m,
		Return15m:          snap.Feature.Return15m,
		VWAP:               snap.Feature.VWAP,
		PriceVsVWAPBps:     snap.Feature.PriceVsVWAPBps,
		VolumeRatio5m:      snap.Feature.VolumeRatio5m,
		SpreadBps:          snap.SpreadBps,
		OrderbookImbalance: snap.Feature.OrderbookImbalance,
		RealizedVol5m:      snap.Feature.RealizedVol5m,
		MarketReturn5m:     snap.Feature.MarketReturn5m,
		SectorReturn5m:     snap.Feature.SectorReturn5m,

		Return3m:                      snap.Feature.Return3m,
		Return30m:                     snap.Feature.Return30m,
		HighDistance5m:                snap.Feature.HighDistance5m,
		LowDistance5m:                 snap.Feature.LowDistance5m,
		SessionHighDistance:           snap.Feature.SessionHighDistance,
		SessionLowDistance:            snap.Feature.SessionLowDistance,
		VWAPSlope:                     snap.Feature.VWAPSlope,
		VWAPCrossDirection:            snap.Feature.VWAPCrossDirection,
		Volume1m:                      snap.Feature.Volume1m,
		Volume5m:                      snap.Feature.Volume5m,
		VolumeRatio1m:                 snap.Feature.VolumeRatio1m,
		Turnover1m:                    snap.Feature.Turnover1m,
		Turnover5m:                    snap.Feature.Turnover5m,
		ATR1m:                         snap.Feature.ATR1m,
		ATR5m:                         snap.Feature.ATR5m,
		RealizedVol15m:                snap.Feature.RealizedVol15m,
		VolatilityExpansionRatio:      snap.Feature.VolatilityExpansionRatio,
		BestBid:                       snap.Bid,
		BestAsk:                       snap.Ask,
		BidDepth:                      snap.Feature.BidDepth,
		AskDepth:                      snap.Feature.AskDepth,
		BuyTradeRatio:                 snap.Feature.BuyTradeRatio,
		SellTradeRatio:                snap.Feature.SellTradeRatio,
		TradeFlowImbalance:            snap.Feature.TradeFlowImbalance,
		Microprice:                    snap.Feature.Microprice,
		MarketReturn1m:                snap.Feature.MarketReturn1m,
		StockVsSectorRelativeStrength: snap.Feature.StockVsSectorRelativeStrength,
		MarketBreadth:                 snap.Feature.MarketBreadth,
	}
}

// ragFeatureInput maps a ScoutState onto rag.FeatureInput (rag/vector.go
// §FeatureInput), the named-field shape RAG standardizes into the fixed
// 14-dimension embedding (FR-RAG-1).
func ragFeatureInput(state ScoutState) rag.FeatureInput {
	priceVsVWAPBps := state.PriceVsVWAPBps
	return rag.FeatureInput{
		Return1m:           state.Return1m,
		Return5m:           state.Return5m,
		Return15m:          state.Return15m,
		PriceVsVWAPBps:     &priceVsVWAPBps,
		VolumeRatio1m:      state.VolumeRatio1m,
		VolumeRatio5m:      state.VolumeRatio5m,
		SpreadBps:          state.SpreadBps,
		OrderbookImbalance: state.OrderbookImbalance,
		RealizedVol5m:      state.RealizedVol5m,
		RealizedVol15m:     state.RealizedVol15m,

		VolatilityExpansionRatio:      state.VolatilityExpansionRatio,
		MarketReturn5m:                state.MarketReturn5m,
		SectorReturn5m:                state.SectorReturn5m,
		StockVsSectorRelativeStrength: state.StockVsSectorRelativeStrength,
	}
}

// ScoutJobPayload is the jev-scout queue job payload
// (jobqueue.JobQueueJevScout): it identifies which instrument to
// evaluate. Handle reads that instrument's latest market_snapshots row
// for the current ScoutState, mirroring internal/service/scheduler's
// fullScanPayload convention of carrying only IDs - not precomputed data -
// in the job payload.
type ScoutJobPayload struct {
	InstrumentID int64  `json:"instrument_id"`
	Symbol       string `json:"symbol"`
}

// Scout evaluates Fast Screener candidates against Jev's FR-SCOUT-1
// question group and persists every completed call to jev_decisions
// (decision_type=scout, FR-SCOUT-3).
type Scout struct {
	client     *Client
	decisions  *judgement.DecisionRepository
	snapshots  *market.SnapshotRepository
	jobs       *jobqueue.JobRepository
	rag        *rag.Service
	thresholds config.JevScoutConfig
	news       NewsSource
}

// NewScout returns a Scout that calls client, persists decisions via
// decisions, reads the latest market state via snapshots, and - for a
// job handled off the jev-scout queue - enqueues a jev-trader job via
// jobs when a candidate passes FR-SCOUT-2 (functional.md §4.4 sequence:
// Scout通過銘柄 -> Jev Trader). jobs may be nil if only Evaluate (not
// HandleJob) will be used. ragService builds the RAG few-shot context
// injected into every Scout request and indexes each persisted decision
// for future searches (functional.md §4.13, FR-RAG-1〜4).
func NewScout(client *Client, decisions *judgement.DecisionRepository, snapshots *market.SnapshotRepository, jobs *jobqueue.JobRepository, ragService *rag.Service, thresholds config.JevScoutConfig, opts ...Option) *Scout {
	o := newOptions(opts)
	return &Scout{
		client:     client,
		decisions:  decisions,
		snapshots:  snapshots,
		jobs:       jobs,
		rag:        ragService,
		thresholds: thresholds,
		news:       o.news,
	}
}

// Evaluate builds the RAG few-shot context for state (FR-RAG-2, FR-RAG-3),
// calls Jev Scout for one instrument's current state, persists the
// resulting jev_decisions row (FR-SCOUT-3) and its standardized feature
// embedding (FR-RAG-1), and reports whether the candidate passed
// FR-SCOUT-2. A Jev API failure (after the Client's retry policy is
// exhausted) returns an error and persists nothing: overview.md §6
// "継続失敗でnew entry停止". RAG context search/indexing failures are
// logged, not returned: a technical fault in the RAG side must never
// block Jev from being called (functional.md FR-RAG-4's cold-start
// tolerance extends to any RAG failure, not only an empty index).
func (s *Scout) Evaluate(ctx context.Context, instrumentID int64, state ScoutState) (domain.JevDecision, bool, error) {
	state = withNewsContext(state, s.news)
	featureInput := ragFeatureInput(state)

	ragContext, err := s.rag.Context(ctx, featureInput, rag.DefaultK)
	if err != nil {
		slog.Error("jev: build rag context failed, calling Scout without similar-case context", "symbol", state.Symbol, "error", err)
	}

	stateJSON, err := json.Marshal(state)
	if err != nil {
		return domain.JevDecision{}, false, fmt.Errorf("jev: encode scout state for %q: %w", state.Symbol, err)
	}

	resp, latency, err := s.client.Scout(ctx, ScoutRequest{QuestionVersion: ScoutQuestionVersion, State: state, RAGContext: ragContext})
	if err != nil {
		return domain.JevDecision{}, false, fmt.Errorf("jev: scout evaluation for %q: %w", state.Symbol, err)
	}

	responseJSON, err := json.Marshal(resp)
	if err != nil {
		return domain.JevDecision{}, false, fmt.Errorf("jev: encode scout response for %q: %w", state.Symbol, err)
	}

	decision := domain.JevDecision{
		InstrumentID:    instrumentID,
		Symbol:          state.Symbol,
		Timestamp:       state.Timestamp,
		DecisionType:    domain.JevDecisionTypeScout,
		StateHash:       hashState(stateJSON),
		StateJSON:       string(stateJSON),
		QuestionVersion: ScoutQuestionVersion,
		ResponseJSON:    string(responseJSON),
		LatencyMs:       int(latency.Milliseconds()),
		ModelID:         resp.ModelID,
		RequestCost:     resp.RequestCost,
	}

	saved, err := s.decisions.Insert(ctx, decision)
	if err != nil {
		return domain.JevDecision{}, false, fmt.Errorf("jev: persist scout decision for %q: %w", state.Symbol, err)
	}

	if err := s.rag.IndexDecision(ctx, saved.ID, featureInput); err != nil {
		slog.Error("jev: index decision vector failed", "decision_id", saved.ID, "symbol", state.Symbol, "error", err)
	}

	return saved, Passes(s.thresholds, resp), nil
}

// HandleJob processes one jev-scout queue job (ScoutJobPayload): it loads
// the instrument's latest market state, evaluates it via Evaluate, and -
// on a FR-SCOUT-2 pass - enqueues a jev-trader job carrying the same
// payload so Jev Trader (a later sub-scope) picks up the candidate next
// (functional.md §4.4). Its signature matches
// internal/service/scheduler.Handler, so it can be registered directly:
// scheduler.RegisterHandler(jobqueue.JobQueueJevScout, scout.HandleJob).
func (s *Scout) HandleJob(ctx context.Context, job jobqueue.Job) error {
	var payload ScoutJobPayload
	if err := json.Unmarshal([]byte(job.PayloadJSON), &payload); err != nil {
		return fmt.Errorf("jev: decode scout job payload: %w", err)
	}

	snapshots, err := s.snapshots.ListByInstrument(ctx, payload.InstrumentID, 1)
	if err != nil {
		return fmt.Errorf("jev: load latest snapshot for %q: %w", payload.Symbol, err)
	}
	if len(snapshots) == 0 {
		return fmt.Errorf("jev: no market snapshot recorded yet for %q", payload.Symbol)
	}

	_, passed, err := s.Evaluate(ctx, payload.InstrumentID, StateFromSnapshot(snapshots[0]))
	if err != nil {
		return err
	}
	if !passed || s.jobs == nil {
		return nil
	}

	traderPayload, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("jev: encode jev-trader job payload for %q: %w", payload.Symbol, err)
	}
	if _, err := s.jobs.Enqueue(ctx, jobqueue.JobQueueJevTrader, string(traderPayload), time.Now().UTC()); err != nil {
		return fmt.Errorf("jev: enqueue jev-trader job for %q: %w", payload.Symbol, err)
	}
	return nil
}

func hashState(stateJSON []byte) string {
	sum := sha256.Sum256(stateJSON)
	return hex.EncodeToString(sum[:])
}
