package bootstrap

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository"
	"github.com/ousiassllc/pitha-trador/internal/service/featureengine"
	"github.com/ousiassllc/pitha-trador/internal/service/marketdata"
)

// marketDataJobPayload mirrors internal/service/scheduler's own unexported
// fullScanPayload: {"instrument_id":..,"symbol":".."}, the JSON body
// EnqueueFullScan enqueues onto both the market-data and feature-calc
// queues (scheduler.go). Kept as a separate type here (rather than an
// exported one in scheduler) since only the JSON shape, not the Go type
// itself, is the real contract between enqueuer and handler.
type marketDataJobPayload struct {
	InstrumentID int64  `json:"instrument_id"`
	Symbol       string `json:"symbol"`
}

// handleMarketData is the market-data queue Handler (issue #44): it
// fetches symbol's current 時価情報・板情報 from kabuステーションAPI,
// computes its Feature values against snapshotHistoryLookback prior bars,
// and persists the result as one market_snapshots row via
// FeatureEngine.RunCycle (which also indexes it for RAG - FR-RAG-1).
//
// This single handler covers both "market data acquisition" and "feature
// computation": featureengine.Engine only exposes an atomic
// fetch-translate-compute-persist step (doc.go), so there is no
// intermediate persisted state a genuinely separate feature-calc handler
// could compute from. See handleFeatureCalc's own doc comment.
//
// A GetBoard failure (kabuステーションAPI not running on a dev machine,
// auth error, network error, ...) is returned as-is: Scheduler's Handler
// contract already marks the job failed and moves on to the next one
// (scheduler.go's processNext) without crashing the process or the other
// queues' workers, satisfying issue #44's "接続失敗時にプロセス全体が
// クラッシュしないことが必須" requirement without this handler needing to
// swallow the error itself.
func (s *Services) handleMarketData(ctx context.Context, job repository.Job) error {
	var payload marketDataJobPayload
	if err := json.Unmarshal([]byte(job.PayloadJSON), &payload); err != nil {
		return fmt.Errorf("bootstrap: decode market-data job payload: %w", err)
	}

	board, err := s.MarketData.GetBoard(ctx, payload.Symbol, defaultKabuExchange)
	if err != nil {
		return fmt.Errorf("bootstrap: fetch board for %q: %w", payload.Symbol, err)
	}

	history, err := s.Snapshots.ListByInstrument(ctx, payload.InstrumentID, snapshotHistoryLookback)
	if err != nil {
		return fmt.Errorf("bootstrap: list snapshot history for %q: %w", payload.Symbol, err)
	}

	rawJSON, err := json.Marshal(board)
	if err != nil {
		return fmt.Errorf("bootstrap: encode raw board data for %q: %w", payload.Symbol, err)
	}

	now := time.Now().UTC()
	input := featureengine.Input{
		Timestamp: now,
		Current:   readingFromBoard(board),
		History:   history,
		// MarketReturn5m/SectorReturn5m require a tracked market/sector
		// index instrument (TOPIX/Nikkei225/sector index); no such
		// instrument-tracking convention exists yet in this codebase, so
		// both stay nil (FR-FE-2's "missing data" nil, not a fabricated
		// placeholder) until a later scope introduces one.
	}

	if _, err := s.FeatureEngine.RunCycle(ctx, []featureengine.CycleInput{{
		InstrumentID: payload.InstrumentID,
		Symbol:       payload.Symbol,
		Input:        input,
		RawDataJSON:  string(rawJSON),
	}}); err != nil {
		return fmt.Errorf("bootstrap: run feature engine cycle for %q: %w", payload.Symbol, err)
	}
	return nil
}

// handleFeatureCalc is the feature-calc queue Handler (issue #44). It is
// an intentional no-op: EnqueueFullScan (scheduler.go, already
// implemented/tested before issue #41) enqueues one feature-calc job
// alongside every market-data job, but handleMarketData above already
// computes and persists Feature as part of its own atomic
// fetch-compute-persist step, since featureengine.Engine.RunCycle exposes
// no separate "compute from an already-persisted raw reading" entry
// point. Registering a handler that simply succeeds (rather than leaving
// the queue unregistered) keeps its jobs from piling up as permanently
// "pending" rows; a future scope that splits Engine into distinct raw/
// compute phases would give this handler real work to do.
func (s *Services) handleFeatureCalc(context.Context, repository.Job) error {
	return nil
}

// readingFromBoard translates a marketdata.Board into the
// featureengine.Reading Compute expects (doc.go: "Callers translate
// marketdata.Board into the featureengine.Reading this package expects").
func readingFromBoard(board marketdata.Board) featureengine.Reading {
	return featureengine.Reading{
		Price:    board.CurrentPrice,
		VWAP:     board.VWAP,
		Volume:   int64(board.TradingVolume),
		Turnover: board.TradingValue,
		Bid:      board.BidPrice,
		Ask:      board.AskPrice,
		BidQty:   board.BidQty,
		AskQty:   board.AskQty,
	}
}
