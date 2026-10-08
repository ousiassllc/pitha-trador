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
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/jobqueue"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/service/broker"
	"github.com/ousiassllc/pitha-trador/internal/service/execution"
	"github.com/ousiassllc/pitha-trador/internal/service/featureengine"
	"github.com/ousiassllc/pitha-trador/internal/service/featureengine/eventtrigger"
	"github.com/ousiassllc/pitha-trador/internal/service/featureengine/marketcontext"
	"github.com/ousiassllc/pitha-trador/internal/service/newsfeed"
	"github.com/ousiassllc/pitha-trador/internal/service/scheduler"
	"github.com/ousiassllc/pitha-trador/internal/service/screener"
)

// QuoteSource returns the latest quote for a symbol (broker.StreamFeed).
type QuoteSource interface {
	Latest(ctx context.Context, symbol string) (broker.Quote, error)
}

// SymbolSource returns a symbol's 銘柄情報 (貸借・値幅制限) for the entry
// eligibility flags (*symbolcache.Cache). Optional on Handler.
type SymbolSource interface {
	Get(ctx context.Context, symbol string) (broker.SymbolInfo, error)
}

// Handler is the market-data queue Handler over the dependencies it uses.
type Handler struct {
	Quotes        QuoteSource
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
	// MarketContextMaxAge is how old an index/stock bar may be and still
	// count as the current market for the market context (issue #692):
	// ScanConfig.SnapshotMaxAge(domain.MaxSnapshotAge). Unset or
	// non-positive means domain.MaxSnapshotAge (marketcontext.NewLoader).
	MarketContextMaxAge time.Duration
	// Symbols supplies each stock's 貸借区分 and 値幅上限/下限 (issue #511).
	// nil leaves the flags unknown, which restricts nothing.
	Symbols SymbolSource
	// Now is the clock stamping each bar (default time.Now). Tests pin it so
	// Execution's session gate does not depend on the wall clock (issue #512).
	Now func() time.Time

	// marketContext shares the market-wide context (index returns, breadth)
	// across all jobs instead of re-reading it per symbol (issue #622); it
	// is built on first use from Instruments and Snapshots.
	marketContextOnce sync.Once
	marketContext     *marketcontext.Loader
}

// marketContextLoader returns the Handler's shared market context Loader.
func (h *Handler) marketContextLoader() *marketcontext.Loader {
	h.marketContextOnce.Do(func() {
		h.marketContext = marketcontext.NewLoader(h.Instruments, h.Snapshots, h.MarketContextMaxAge)
	})
	return h.marketContext
}

// now is the current UTC time from Handler.Now (time.Now when unset).
func (h *Handler) now() time.Time {
	if h.Now == nil {
		return time.Now().UTC()
	}
	return h.Now().UTC()
}

// marketDataJobPayload mirrors scheduler's unexported fullScanPayload
// ({"instrument_id":..,"symbol":".."}); only the JSON shape is the contract.
type marketDataJobPayload struct {
	InstrumentID int64  `json:"instrument_id"`
	Symbol       string `json:"symbol"`
}

// HandleMarketData is the market-data queue Handler (issue #44): it
// fetches symbol's current 時価情報・板情報 (the fresh PUSH board else a REST
// poll; never a price-0 quote - Quotes.Latest), computes its Feature
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
	pt := newPhaseTimer()
	defer pt.warnIfSlow(payload.Symbol)

	quote, err := h.Quotes.Latest(ctx, payload.Symbol)
	if err != nil {
		if errors.Is(err, broker.ErrRateLimited) {
			slog.Warn("marketdatajob: defer board fetch to next cycle (broker info api rate limited)",
				"symbol", payload.Symbol)
			return nil
		}
		return fmt.Errorf("marketdatajob: fetch board for %q: %w", payload.Symbol, err)
	}
	pt.mark("latest")

	history, err := h.Snapshots.ListHistoryByInstrument(ctx, payload.InstrumentID, featureengine.HistoryLookbackBars)
	if err != nil {
		return fmt.Errorf("marketdatajob: list snapshot history for %q: %w", payload.Symbol, err)
	}
	pt.mark("history")

	rawJSON, err := json.Marshal(quote.Raw)
	if err != nil {
		return fmt.Errorf("marketdatajob: encode raw board data for %q: %w", payload.Symbol, err)
	}

	inst, err := h.Instruments.Get(ctx, payload.InstrumentID)
	if err != nil {
		return fmt.Errorf("marketdatajob: load instrument %q: %w", payload.Symbol, err)
	}

	now := h.now()
	mc := h.marketContextLoader().Load(ctx, inst, now)
	pt.mark("context")
	current := readingFromQuote(quote)
	h.applySymbolInfo(ctx, &current, inst, payload.Symbol)
	pt.mark("symbol")
	input := featureengine.Input{
		Timestamp:      now,
		Current:        current,
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
	pt.mark("cycle")

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
	pt.mark("execution")
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

// applySymbolInfo fills r's 貸借区分 and stop-high/stop-low flag from the
// stock's 銘柄情報. It is best effort: a failed lookup leaves them unknown
// (Lendable nil, PriceLimitNone), because a missing flag must never stop
// the bar from being persisted; the failure is logged and retried next
// cycle (symbolcache.Cache does not cache errors). Index instruments have no
// 貸借/値幅 and are skipped.
func (h *Handler) applySymbolInfo(ctx context.Context, r *featureengine.Reading, inst domain.Instrument, symbol string) {
	if h.Symbols == nil || inst.Kind != domain.InstrumentKindStock {
		return
	}
	info, err := h.Symbols.Get(ctx, symbol)
	if err != nil {
		slog.Warn("marketdatajob: fetch symbol info failed; lendable/price-limit unknown", "symbol", symbol, "error", err)
		return
	}
	r.Lendable = info.Lendable
	r.PriceLimit = info.PriceLimit(r.Price)
}

// readingFromQuote translates a broker.Quote into the featureengine.Reading
// Compute expects (featureengine doc.go: "Callers translate the broker's
// quote into the featureengine.Reading this package expects"). Bid/Ask already
// use the conventional meaning (Bid = best buy quote, Ask = best sell quote);
// the broker adapter owns any naming quirk of its API, so this is a plain
// field-by-field mapping.
func readingFromQuote(q broker.Quote) featureengine.Reading {
	return featureengine.Reading{
		Price:        q.Price,
		VWAP:         q.VWAP,
		Volume:       int64(q.Volume),
		Turnover:     q.Turnover,
		SessionHigh:  q.High,
		SessionLow:   q.Low,
		Bid:          q.Bid,
		Ask:          q.Ask,
		BidQty:       q.BidQty,
		AskQty:       q.AskQty,
		BidDepth:     q.BidDepth,
		AskDepth:     q.AskDepth,
		SpecialQuote: q.SpecialQuote,
	}
}
