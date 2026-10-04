// Package marketdatajob holds the market-data and feature-calc scheduler
// queue handlers (issue #44): fetch the latest board, compute and persist
// the Feature snapshot, run Paper Trading's per-bar step and enqueue
// FR-SCAN-1's event-driven re-evaluation. It lives under internal/bootstrap
// as composition-root glue; Handler takes only the dependencies it uses
// (docs/architecture/overview.md §3).
package marketdatajob

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/jobqueue"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/service/execution"
	"github.com/ousiassllc/pitha-trador/internal/service/featureengine"
	"github.com/ousiassllc/pitha-trador/internal/service/featureengine/eventtrigger"
	"github.com/ousiassllc/pitha-trador/internal/service/marketdata"
	"github.com/ousiassllc/pitha-trador/internal/service/newsfeed"
	"github.com/ousiassllc/pitha-trador/internal/service/scheduler"
	"github.com/ousiassllc/pitha-trador/internal/service/screener"
)

// BoardSource returns the latest board for a symbol (*pushfeed.Feed).
type BoardSource interface {
	Latest(ctx context.Context, symbol string) (marketdata.Board, error)
}

// Handler is the market-data queue Handler over the dependencies it uses.
type Handler struct {
	Boards        BoardSource
	Instruments   *market.InstrumentRepository
	Snapshots     *market.SnapshotRepository
	FeatureEngine *featureengine.Engine
	Execution     *execution.Engine
	Screener      *screener.LiveSource
	News          *newsfeed.Service
	Scheduler     *scheduler.Scheduler
	// EventTrigger holds FR-SCAN-1/FR-SCAN-2's thresholds
	// (config/strategy.yaml scan.event_trigger).
	EventTrigger config.EventTriggerConfig
}

// marketDataJobPayload mirrors scheduler's unexported fullScanPayload
// ({"instrument_id":..,"symbol":".."}); only the JSON shape is the contract.
type marketDataJobPayload struct {
	InstrumentID int64  `json:"instrument_id"`
	Symbol       string `json:"symbol"`
}

// HandleMarketData is the market-data queue Handler (issue #44): it
// fetches symbol's current 時価情報・板情報 (the fresh PUSH board else a REST
// poll; never a price-0 board - Boards.Latest), computes its Feature
// values against featureengine.HistoryLookbackBars prior bars, persists
// one market_snapshots row via FeatureEngine.RunCycle (which also indexes
// it for RAG - FR-RAG-1), then runs Paper Trading's position management
// against that bar (execution.Engine.OnSnapshot). It covers both
// acquisition and feature computation (see HandleFeatureCalc).
//
// A board failure is returned as-is: Scheduler marks the job failed and
// moves on without crashing the process (issue #44 "接続失敗時にプロセス全体が
// クラッシュしないことが必須").
func (h *Handler) HandleMarketData(ctx context.Context, job jobqueue.Job) error {
	var payload marketDataJobPayload
	if err := json.Unmarshal([]byte(job.PayloadJSON), &payload); err != nil {
		return fmt.Errorf("marketdatajob: decode market-data job payload: %w", err)
	}

	board, err := h.Boards.Latest(ctx, payload.Symbol)
	if err != nil {
		return fmt.Errorf("marketdatajob: fetch board for %q: %w", payload.Symbol, err)
	}

	history, err := h.Snapshots.ListByInstrument(ctx, payload.InstrumentID, featureengine.HistoryLookbackBars)
	if err != nil {
		return fmt.Errorf("marketdatajob: list snapshot history for %q: %w", payload.Symbol, err)
	}

	rawJSON, err := json.Marshal(board)
	if err != nil {
		return fmt.Errorf("marketdatajob: encode raw board data for %q: %w", payload.Symbol, err)
	}

	inst, err := h.Instruments.Get(ctx, payload.InstrumentID)
	if err != nil {
		return fmt.Errorf("marketdatajob: load instrument %q: %w", payload.Symbol, err)
	}

	now := time.Now().UTC()
	mc := featureengine.NewMarketContextLoader(h.Instruments, h.Snapshots).Load(ctx, inst, now)
	input := featureengine.Input{
		Timestamp:      now,
		Current:        readingFromBoard(board),
		History:        history,
		MarketReturn1m: mc.MarketReturn1m,
		MarketReturn5m: mc.MarketReturn5m,
		SectorReturn5m: mc.SectorReturn5m,
		MarketBreadth:  mc.MarketBreadth,
	}

	persisted, err := h.FeatureEngine.RunCycle(ctx, []featureengine.CycleInput{{
		InstrumentID: payload.InstrumentID,
		Symbol:       payload.Symbol,
		Input:        input,
		RawDataJSON:  string(rawJSON),
	}})
	if err != nil {
		return fmt.Errorf("marketdatajob: run feature engine cycle for %q: %w", payload.Symbol, err)
	}

	// Paper Trading's per-bar step (issue #49): fill crossed limit
	// entries, mark the open position to market, and close it when an
	// FR-EXIT-1 condition triggers against this fresh bar.
	for _, snap := range persisted {
		if _, err := h.Execution.OnSnapshot(ctx, snap); err != nil {
			return fmt.Errorf("marketdatajob: manage paper position for %q: %w", payload.Symbol, err)
		}
		if err := h.enqueueEventReevaluation(ctx, snap, history); err != nil {
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
func (h *Handler) enqueueEventReevaluation(ctx context.Context, snap domain.Snapshot, history []domain.Snapshot) error {
	if len(history) == 0 || !h.isCandidate(ctx, snap.InstrumentID) {
		return nil
	}
	trigger := h.EventTrigger
	signal := eventtrigger.Detect(history[0], snap, history, eventtrigger.Thresholds{
		Return1mChange:           trigger.Return1mChangeThreshold,
		VolumeRatioChange:        trigger.VolumeRatioChangeThreshold,
		SpreadChangeBps:          trigger.SpreadChangeBpsThreshold,
		OrderbookImbalanceChange: trigger.OrderbookImbalanceChangeThreshold,
		TradeFlowImbalanceChange: trigger.TradeFlowImbalanceChangeThreshold,
	}, h.News.TakeNewsFlag(snap.Symbol))
	if err := h.Scheduler.EnqueueEventReevaluation(ctx, snap.InstrumentID, snap.Symbol, signal.Triggered(), snap.Timestamp); err != nil {
		return fmt.Errorf("marketdatajob: event-driven reevaluation for %q: %w", snap.Symbol, err)
	}
	return nil
}

func (h *Handler) isCandidate(ctx context.Context, instrumentID int64) bool {
	candidates, _, _ := h.Screener.Candidates(ctx)
	for _, c := range candidates {
		if c.InstrumentID == instrumentID {
			return true
		}
	}
	return false
}

// HandleFeatureCalc is the feature-calc queue Handler (issue #44), an
// intentional no-op: Handler.HandleMarketData already computes and persists
// Feature atomically. EnqueueFullScan no longer feeds this queue; the
// handler remains so feature-calc jobs left by an earlier version drain
// instead of staying "pending" forever.
func HandleFeatureCalc(context.Context, jobqueue.Job) error {
	return nil
}

// readingFromBoard translates a marketdata.Board into the
// featureengine.Reading Compute expects (doc.go: "Callers translate
// marketdata.Board into the featureengine.Reading this package expects").
//
// kabuステーションAPI names the best quotes from the trader's side: BidPrice/
// BidQty is the best SELL (offer) quote and AskPrice/AskQty the best BUY
// (bid) quote (kabu_STATION_API.yaml BoardSuccess), the reverse of the
// conventional meaning featureengine.Reading uses (Bid = best buy quote,
// Ask = best sell quote). The mapping is therefore swapped here, and
// likewise the depth: Buy1..10 is Reading.BidDepth, Sell1..10 AskDepth.
func readingFromBoard(board marketdata.Board) featureengine.Reading {
	r := featureengine.Reading{
		Price:       board.CurrentPrice,
		VWAP:        board.VWAP,
		Volume:      int64(board.TradingVolume),
		Turnover:    board.TradingValue,
		SessionHigh: board.HighPrice,
		SessionLow:  board.LowPrice,
		Bid:         board.AskPrice,
		Ask:         board.BidPrice,
		BidQty:      board.AskQty,
		AskQty:      board.BidQty,
	}
	if d, ok := board.BuyDepth(); ok {
		r.BidDepth = &d
	}
	if d, ok := board.SellDepth(); ok {
		r.AskDepth = &d
	}
	return r
}
