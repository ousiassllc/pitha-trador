// Package bootstrap's services.go (extended incrementally by issue #41's
// other split children, #44 onward) is the composition root that builds
// every internal/service/* instance cmd/desktop and cmd/server share, and
// registers each internal/service/scheduler queue's Handler. BuildServices
// only *constructs* everything; nothing in it actually runs a background
// goroutine (kabuステーションAPI token refresh, Scheduler workers/cron)
// until a later sub-scope (#51) adds the corresponding cmd/ OnStartup/main
// call to (*Services).Start.
package bootstrap

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/repository"
	"github.com/ousiassllc/pitha-trador/internal/service/featureengine"
	"github.com/ousiassllc/pitha-trador/internal/service/marketdata"
	"github.com/ousiassllc/pitha-trador/internal/service/rag"
	"github.com/ousiassllc/pitha-trador/internal/service/scheduler"
)

// defaultTokenRefreshInterval is how often Services.Start reissues the
// kabuステーションAPI token (marketdata.Client.Start). kabuステーション
// API's own reference does not publish an exact token TTL
// (docs/architecture/overview.md §5 only says "有効期限があるため...
// 定期的に再発行"); 20 minutes is a conservative guess that comfortably
// reissues well before any plausible expiry while staying well above
// fullScanInterval (60s) so it never dominates request volume.
const defaultTokenRefreshInterval = 20 * time.Minute

// snapshotHistoryLookback is how many prior market_snapshots bars
// handleMarketData fetches per instrument to build
// featureengine.Input.History. Compute's longest lookback window is
// Return15m/RealizedVol5m; at a 60s full-scan cadence, 20 bars covers 20
// minutes of history - comfortably past every window Compute currently
// uses (functional.md §4.1's longest window is 15m).
const snapshotHistoryLookback = 20

// defaultKabuExchange is the kabuステーションAPI market code every
// instrument is queried under. This build's target universe is TSE-listed
// equities only (docs/requirements, docs/architecture/overview.md do not
// describe multi-exchange support), so instruments.Market (a free-text
// display string such as "TSE Prime") is not translated into a per-row
// exchange code; every symbol uses marketdata.ExchangeTSE.
const defaultKabuExchange = marketdata.ExchangeTSE

// Services holds every internal/service/* instance the composition root
// builds, plus the repositories they share. cmd/desktop and cmd/server
// both call BuildServices once (after bootstrap.Run) and derive their
// internal/router.New options and Wails/http lifecycle hooks from the
// returned *Services.
type Services struct {
	Instruments *repository.InstrumentRepository
	Snapshots   *repository.SnapshotRepository
	Decisions   *repository.DecisionRepository
	Jobs        *repository.JobRepository

	RAG           *rag.Service
	MarketData    *marketdata.Client
	FeatureEngine *featureengine.Engine

	Scheduler *scheduler.Scheduler

	strategy *config.StrategyConfig
}

// BuildServices constructs the full composition-root service graph on top
// of state (bootstrap.Run's DB + config) and secrets (config.LoadSecrets),
// registering every internal/service/scheduler queue Handler this build
// wires (market-data, feature-calc). It performs no I/O itself (no DB
// queries beyond what the repository constructors below do, which is
// none - they only hold *sql.DB) and starts no goroutine; see
// (*Services).Start.
func BuildServices(state *State, secrets config.Secrets) (*Services, error) {
	instruments := repository.NewInstrumentRepository(state.DB)
	snapshots := repository.NewSnapshotRepository(state.DB)
	decisions := repository.NewDecisionRepository(state.DB)
	jobs := repository.NewJobRepository(state.DB)

	ragService := rag.NewService(state.DB, decisions, snapshots)
	featureEngine := featureengine.NewEngine(snapshots, ragService)

	marketDataClient := marketdata.NewClient(marketdata.Config{
		APIPassword: secrets.KabuAPIPassword,
	})

	sched := scheduler.New(jobs, instruments)

	svc := &Services{
		Instruments:   instruments,
		Snapshots:     snapshots,
		Decisions:     decisions,
		Jobs:          jobs,
		RAG:           ragService,
		MarketData:    marketDataClient,
		FeatureEngine: featureEngine,
		Scheduler:     sched,
		strategy:      state.Strategy,
	}

	sched.RegisterHandler(repository.JobQueueMarketData, svc.handleMarketData)
	sched.RegisterHandler(repository.JobQueueFeatureCalc, svc.handleFeatureCalc)

	return svc, nil
}

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

// Start launches every background goroutine this build's composition
// root owns: kabuステーションAPI token issuance/refresh
// (marketdata.Client.Start) and the Scheduler's worker pool + full-scan/
// self-improve/log-rotation cron triggers (scheduler.Scheduler.Start),
// after first recovering any job left "running" by a previous crash
// (scheduler.Scheduler.Recover). Both run until ctx is done or Stop is
// called. Neither cmd/desktop nor cmd/server calls this yet (issue #51
// wires the first such call site, in Wails' OnStartup / cmd/server's
// main respectively) - BuildServices only constructs, it does not start.
func (s *Services) Start(ctx context.Context) error {
	if _, err := s.Scheduler.Recover(ctx); err != nil {
		return fmt.Errorf("bootstrap: recover jobs: %w", err)
	}

	if err := s.MarketData.Start(ctx, defaultTokenRefreshInterval); err != nil {
		// kabuステーションAPI not reachable at startup (dev machine
		// without the kabuステーションアプリ running, issue #44's own
		// scope note) must not prevent the rest of the process (Scanner
		// Dashboard, API, other queues) from starting: log and continue
		// with no token. Every subsequent GetBoard call simply fails
		// (ErrNoToken or a request error) and handleMarketData's own
		// per-job error handling already covers that.
		slog.Error("bootstrap: kabuステーションAPI initial token issuance failed, continuing without a token", "error", err)
	}

	fullScanInterval := time.Duration(s.strategy.Scan.FullScanIntervalSeconds) * time.Second
	if err := s.Scheduler.Start(ctx, fullScanInterval); err != nil {
		return fmt.Errorf("bootstrap: start scheduler: %w", err)
	}

	return nil
}

// Stop stops the Scheduler's worker pool and cron triggers, blocking
// until every worker goroutine has exited (scheduler.Scheduler.Stop).
// marketdata.Client.Start's own token-refresh goroutine is tied to the
// ctx passed to Start above, not to this method - callers cancel that
// same ctx (e.g. via context.WithCancel in main/OnStartup, canceled from
// OnShutdown) to stop it.
func (s *Services) Stop() {
	s.Scheduler.Stop()
}
