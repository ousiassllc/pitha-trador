package bootstrap

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository"
	"github.com/ousiassllc/pitha-trador/internal/service/featureengine"
	"github.com/ousiassllc/pitha-trador/internal/service/featureengine/eventtrigger"
	"github.com/ousiassllc/pitha-trador/internal/service/marketdata"
)

// marketDataJobPayload mirrors scheduler's unexported fullScanPayload
// ({"instrument_id":..,"symbol":".."}); only the JSON shape is the contract.
type marketDataJobPayload struct {
	InstrumentID int64  `json:"instrument_id"`
	Symbol       string `json:"symbol"`
}

// handleMarketData is the market-data queue Handler (issue #44): it
// fetches symbol's current 時価情報・板情報 (the fresh PUSH board else a REST
// poll; never a price-0 board - PushFeed.Latest), computes its Feature
// values against featureengine.HistoryLookbackBars prior bars, persists
// one market_snapshots row via FeatureEngine.RunCycle (which also indexes
// it for RAG - FR-RAG-1), then runs Paper Trading's position management
// against that bar (execution.Engine.OnSnapshot). It covers both
// acquisition and feature computation (see handleFeatureCalc).
//
// A board failure is returned as-is: Scheduler marks the job failed and
// moves on without crashing the process (issue #44 "接続失敗時にプロセス全体が
// クラッシュしないことが必須").
func (s *Services) handleMarketData(ctx context.Context, job repository.Job) error {
	var payload marketDataJobPayload
	if err := json.Unmarshal([]byte(job.PayloadJSON), &payload); err != nil {
		return fmt.Errorf("bootstrap: decode market-data job payload: %w", err)
	}

	board, err := s.PushFeed.Latest(ctx, payload.Symbol)
	if err != nil {
		return fmt.Errorf("bootstrap: fetch board for %q: %w", payload.Symbol, err)
	}

	history, err := s.Snapshots.ListByInstrument(ctx, payload.InstrumentID, featureengine.HistoryLookbackBars)
	if err != nil {
		return fmt.Errorf("bootstrap: list snapshot history for %q: %w", payload.Symbol, err)
	}

	rawJSON, err := json.Marshal(board)
	if err != nil {
		return fmt.Errorf("bootstrap: encode raw board data for %q: %w", payload.Symbol, err)
	}

	inst, err := s.Instruments.Get(ctx, payload.InstrumentID)
	if err != nil {
		return fmt.Errorf("bootstrap: load instrument %q: %w", payload.Symbol, err)
	}

	now := time.Now().UTC()
	mc := featureengine.NewMarketContextLoader(s.Instruments, s.Snapshots).Load(ctx, inst, now)
	input := featureengine.Input{
		Timestamp:      now,
		Current:        readingFromBoard(board),
		History:        history,
		MarketReturn1m: mc.MarketReturn1m,
		MarketReturn5m: mc.MarketReturn5m,
		SectorReturn5m: mc.SectorReturn5m,
		MarketBreadth:  mc.MarketBreadth,
	}

	persisted, err := s.FeatureEngine.RunCycle(ctx, []featureengine.CycleInput{{
		InstrumentID: payload.InstrumentID,
		Symbol:       payload.Symbol,
		Input:        input,
		RawDataJSON:  string(rawJSON),
	}})
	if err != nil {
		return fmt.Errorf("bootstrap: run feature engine cycle for %q: %w", payload.Symbol, err)
	}

	// Paper Trading's per-bar step (issue #49): fill crossed limit
	// entries, mark the open position to market, and close it when an
	// FR-EXIT-1 condition triggers against this fresh bar.
	for _, snap := range persisted {
		if _, err := s.Execution.OnSnapshot(ctx, snap); err != nil {
			return fmt.Errorf("bootstrap: manage paper position for %q: %w", payload.Symbol, err)
		}
		if err := s.enqueueEventReevaluation(ctx, snap, history); err != nil {
			return err
		}
	}
	return nil
}

// enqueueEventReevaluation implements FR-SCAN-1/FR-SCAN-2 for one freshly
// persisted bar: when snap is a current Fast Screener candidate and
// eventtrigger.Detect against its previous bar fires, a jev-scout
// job is enqueued immediately instead of waiting for the next
// candidate-refresh cycle; otherwise nothing is enqueued (FR-SCAN-2's
// suppression). Non-candidates are skipped: Jev is only ever consulted for
// symbols that passed Fast Screener (functional.md §2's main flow).
// history is snap's prior bars, most recent first. The news signal is
// News Ingest's per-symbol flag (FR-LUNA-3; consumed here so each new
// article triggers one re-evaluation).
func (s *Services) enqueueEventReevaluation(ctx context.Context, snap domain.Snapshot, history []domain.Snapshot) error {
	if len(history) == 0 || !s.isCandidate(ctx, snap.InstrumentID) {
		return nil
	}
	trigger := s.strategy.Scan.EventTrigger
	signal := eventtrigger.Detect(history[0], snap, history, eventtrigger.Thresholds{
		Return1mChange:           trigger.Return1mChangeThreshold,
		VolumeRatioChange:        trigger.VolumeRatioChangeThreshold,
		SpreadChangeBps:          trigger.SpreadChangeBpsThreshold,
		OrderbookImbalanceChange: trigger.OrderbookImbalanceChangeThreshold,
		TradeFlowImbalanceChange: trigger.TradeFlowImbalanceChangeThreshold,
	}, s.News.TakeNewsFlag(snap.Symbol))
	if err := s.Scheduler.EnqueueEventReevaluation(ctx, snap.InstrumentID, snap.Symbol, signal.Triggered(), snap.Timestamp); err != nil {
		return fmt.Errorf("bootstrap: event-driven reevaluation for %q: %w", snap.Symbol, err)
	}
	return nil
}

func (s *Services) isCandidate(ctx context.Context, instrumentID int64) bool {
	candidates, _, _ := s.Screener.Candidates(ctx)
	for _, c := range candidates {
		if c.InstrumentID == instrumentID {
			return true
		}
	}
	return false
}

// handleFeatureCalc is the feature-calc queue Handler (issue #44), an
// intentional no-op: handleMarketData already computes and persists
// Feature atomically; a succeeding handler keeps the feature-calc jobs
// EnqueueFullScan adds from piling up as "pending" rows.
func (s *Services) handleFeatureCalc(context.Context, repository.Job) error {
	return nil
}

// readingFromBoard translates a marketdata.Board into the
// featureengine.Reading Compute expects (doc.go: "Callers translate
// marketdata.Board into the featureengine.Reading this package expects").
func readingFromBoard(board marketdata.Board) featureengine.Reading {
	r := featureengine.Reading{
		Price:       board.CurrentPrice,
		VWAP:        board.VWAP,
		Volume:      int64(board.TradingVolume),
		Turnover:    board.TradingValue,
		SessionHigh: board.HighPrice,
		SessionLow:  board.LowPrice,
		Bid:         board.BidPrice,
		Ask:         board.AskPrice,
		BidQty:      board.BidQty,
		AskQty:      board.AskQty,
	}
	// Sell levels sit with BidPrice/BidQty, Buy levels with AskPrice/AskQty.
	if d, ok := board.SellDepth(); ok {
		r.BidDepth = &d
	}
	if d, ok := board.BuyDepth(); ok {
		r.AskDepth = &d
	}
	return r
}
