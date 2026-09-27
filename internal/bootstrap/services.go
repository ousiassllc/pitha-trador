// Package bootstrap's services.go (extended incrementally by issue #41's
// other split children, #44 onward) is the composition root that builds
// every internal/service/* instance cmd/desktop and cmd/server share, and
// registers each internal/service/scheduler queue's Handler. BuildServices
// only *constructs* everything; nothing in it actually runs a background
// goroutine (kabuステーションAPI token refresh, Scheduler workers/cron)
// until a later sub-scope (#51) adds the corresponding cmd/ OnStartup/main
// call to (*Services).Start (lifecycle.go). Queue Handlers live in
// marketdata_job.go; the Scanner Dashboard candidate-refresh cycle lives
// in candidates.go.
package bootstrap

import (
	"sync"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/repository"
	"github.com/ousiassllc/pitha-trador/internal/service/featureengine"
	"github.com/ousiassllc/pitha-trador/internal/service/marketdata"
	"github.com/ousiassllc/pitha-trador/internal/service/rag"
	"github.com/ousiassllc/pitha-trador/internal/service/scheduler"
	"github.com/ousiassllc/pitha-trador/internal/service/screener"
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

// turnoverTrailingBars is how many of the most recent 1-minute
// market_snapshots bars refreshCandidates sums to build
// screener.Input.Turnover5mJPY. Snapshot.Turnover is itself a per-bar (not
// cumulative-session) value (domain/snapshot.go: "the raw market data
// captured for one Instrument at one 1-minute-bar Timestamp"), so summing
// the most recent 5 bars is exactly the trailing-5-minute turnover
// screener.PassesFilter's liquidity floor expects.
const turnoverTrailingBars = 5

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
	Screener      *screener.LiveSource

	Scheduler *scheduler.Scheduler

	strategy *config.StrategyConfig
	wg       sync.WaitGroup
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
		Screener:      screener.NewLiveSource(),
		Scheduler:     sched,
		strategy:      state.Strategy,
	}

	sched.RegisterHandler(repository.JobQueueMarketData, svc.handleMarketData)
	sched.RegisterHandler(repository.JobQueueFeatureCalc, svc.handleFeatureCalc)

	return svc, nil
}
