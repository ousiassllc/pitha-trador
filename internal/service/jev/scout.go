package jev

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository"
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
	}
}

// ScoutJobPayload is the jev-scout queue job payload
// (repository.JobQueueJevScout): it identifies which instrument to
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
	decisions  *repository.DecisionRepository
	snapshots  *repository.SnapshotRepository
	jobs       *repository.JobRepository
	thresholds config.JevScoutConfig
}

// NewScout returns a Scout that calls client, persists decisions via
// decisions, reads the latest market state via snapshots, and - for a
// job handled off the jev-scout queue - enqueues a jev-trader job via
// jobs when a candidate passes FR-SCOUT-2 (functional.md §4.4 sequence:
// Scout通過銘柄 -> Jev Trader). jobs may be nil if only Evaluate (not
// HandleJob) will be used.
func NewScout(client *Client, decisions *repository.DecisionRepository, snapshots *repository.SnapshotRepository, jobs *repository.JobRepository, thresholds config.JevScoutConfig) *Scout {
	return &Scout{
		client:     client,
		decisions:  decisions,
		snapshots:  snapshots,
		jobs:       jobs,
		thresholds: thresholds,
	}
}

// Evaluate calls Jev Scout for one instrument's current state, persists
// the resulting jev_decisions row (FR-SCOUT-3), and reports whether the
// candidate passed FR-SCOUT-2. A Jev API failure (after the Client's
// retry policy is exhausted) returns an error and persists nothing:
// overview.md §6 "継続失敗でnew entry停止".
func (s *Scout) Evaluate(ctx context.Context, instrumentID int64, state ScoutState) (domain.JevDecision, bool, error) {
	stateJSON, err := json.Marshal(state)
	if err != nil {
		return domain.JevDecision{}, false, fmt.Errorf("jev: encode scout state for %q: %w", state.Symbol, err)
	}

	resp, latency, err := s.client.Scout(ctx, ScoutRequest{QuestionVersion: ScoutQuestionVersion, State: state})
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

	return saved, Passes(s.thresholds, resp), nil
}

// HandleJob processes one jev-scout queue job (ScoutJobPayload): it loads
// the instrument's latest market state, evaluates it via Evaluate, and -
// on a FR-SCOUT-2 pass - enqueues a jev-trader job carrying the same
// payload so Jev Trader (a later sub-scope) picks up the candidate next
// (functional.md §4.4). Its signature matches
// internal/service/scheduler.Handler, so it can be registered directly:
// scheduler.RegisterHandler(repository.JobQueueJevScout, scout.HandleJob).
func (s *Scout) HandleJob(ctx context.Context, job repository.Job) error {
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
	if _, err := s.jobs.Enqueue(ctx, repository.JobQueueJevTrader, string(traderPayload), time.Now().UTC()); err != nil {
		return fmt.Errorf("jev: enqueue jev-trader job for %q: %w", payload.Symbol, err)
	}
	return nil
}

func hashState(stateJSON []byte) string {
	sum := sha256.Sum256(stateJSON)
	return hex.EncodeToString(sum[:])
}
