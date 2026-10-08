// Package bootstrap's services.go (extended incrementally by issue #41's
// other split children, #44 onward) is the composition root that builds
// every internal/service/* instance cmd/desktop and cmd/server share, and
// registers each internal/service/scheduler queue's Handler. BuildServices
// only *constructs* everything; nothing in it runs a background goroutine
// (kabuステーションAPI token refresh, Scheduler workers/cron) until
// cmd/desktop's Wails OnStartup or cmd/server's main calls
// (*Services).Start (lifecycle.go). Queue Handlers live in the
// marketdatajob subpackage; the Scanner Dashboard candidate-refresh cycle
// lives in candidates.
package bootstrap

import (
	"sync"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap/alerts"
	"github.com/ousiassllc/pitha-trador/internal/bootstrap/backtestsource"
	"github.com/ousiassllc/pitha-trador/internal/bootstrap/candidates"
	"github.com/ousiassllc/pitha-trador/internal/bootstrap/marketdatajob"
	"github.com/ousiassllc/pitha-trador/internal/bootstrap/rankingwatch"
	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/config/tachibanasource"
	"github.com/ousiassllc/pitha-trador/internal/logging"
	calrepo "github.com/ousiassllc/pitha-trador/internal/repository/calibration"
	"github.com/ousiassllc/pitha-trador/internal/repository/jobqueue"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/repository/trading"
	"github.com/ousiassllc/pitha-trador/internal/service/activityfeed"
	"github.com/ousiassllc/pitha-trador/internal/service/broker"
	"github.com/ousiassllc/pitha-trador/internal/service/calibration"
	"github.com/ousiassllc/pitha-trador/internal/service/execution"
	"github.com/ousiassllc/pitha-trador/internal/service/featureengine"
	"github.com/ousiassllc/pitha-trador/internal/service/insight"
	"github.com/ousiassllc/pitha-trador/internal/service/jev"
	"github.com/ousiassllc/pitha-trador/internal/service/newsfeed"
	"github.com/ousiassllc/pitha-trador/internal/service/policy"
	"github.com/ousiassllc/pitha-trador/internal/service/rag"
	"github.com/ousiassllc/pitha-trador/internal/service/risk"
	"github.com/ousiassllc/pitha-trador/internal/service/scheduler"
	"github.com/ousiassllc/pitha-trador/internal/service/screener"
	"github.com/ousiassllc/pitha-trador/internal/service/selfimprove"
	"github.com/ousiassllc/pitha-trador/internal/service/updater"
)

// Services holds every internal/service/* instance the composition root
// builds, plus the repositories they share. cmd/desktop and cmd/server
// both call BuildServices once (after bootstrap.Run) and derive their
// internal/router.New options and Wails/http lifecycle hooks from the
// returned *Services.
type Services struct {
	Instruments *market.InstrumentRepository
	Snapshots   *market.SnapshotRepository
	Decisions   *judgement.DecisionRepository
	Signals     *trading.SignalRepository
	Jobs        *jobqueue.JobRepository
	Positions   *trading.PositionRepository
	Orders      *trading.OrderRepository
	Outcomes    *calrepo.CalibrationRepository
	KillSwitch  *system.KillSwitchRepository
	Settings    *system.RuntimeSettingsRepository
	Proposals   *judgement.ProposalRepository

	// DailyBars/DailyBarRuns store the 立花 nightly 日足 batch (issue #729).
	DailyBars    *market.DailyBarRepository
	DailyBarRuns *market.DailyBarRunRepository
	// WatchLists stores the 立花 監視リスト decided each night (issue #730).
	WatchLists *market.WatchListRepository

	RAG           *rag.Service
	Broker        broker.Broker // the selected broker adapter (newBroker; kabu until the 立花 adapter, #724)
	FeatureEngine *featureengine.Engine
	Screener      *screener.LiveSource
	Jev           *jev.Client
	Scout         *jev.Scout
	Trader        *jev.Trader
	News          *newsfeed.Service
	Policy        *policy.Engine
	Risk          *risk.Engine
	Execution     *execution.Engine
	Insight       *insight.Reader
	Calibration   *calibration.Service
	Backtest      *backtestsource.Source
	Activity      *activityfeed.Service
	Governor      *selfimprove.Governor
	ErrorLogs     *logging.Exporter // read-only export of State.Paths.LogDir (FR-ERRLOG-2)

	Scheduler *scheduler.Scheduler
	Updater   *updater.SchedulerAdapter // nil on cmd/server (issue #76)

	strategy *config.StrategyConfig
	wg       sync.WaitGroup

	// tachibana are the 立花 監視銘柄ソース settings read at start-up (the
	// defaults unless 立花 is selected); the nightly daily-bar batch reads them.
	tachibana tachibanasource.TachibanaSourceSettings

	newsEnabled bool
	candidates  *candidates.Refresher
	// watchlist/rankingWatcher are set unless scan.full_scan_enabled is true
	// (rankingwatch_start.go).
	watchlist      *rankingwatch.Watchlist
	rankingWatcher *rankingwatch.Watcher
}

// buildSettings collects BuildServices' optional inputs; see BuildOption.
type buildSettings struct {
	autoUpdate     updater.Quitter
	notifiers      []risk.Notifier
	jevMaxAttempts int
	executionNow   func() time.Time
	yanoshinURL    string
	kabuURL        string
	newsNow        func() time.Time
	brokerSettings config.BrokerSettings
}

// BuildOption customises BuildServices' optional inputs.
type BuildOption func(*buildSettings)

// WithAutoUpdate wires internal/service/updater's periodic self-update check
// onto Scheduler (issue #65, cmd/desktop only; cmd/server leaves it out), also
// exposed as Services.Updater (issue #76).
func WithAutoUpdate(q updater.Quitter) BuildOption {
	return func(s *buildSettings) { s.autoUpdate = q }
}

// WithNotifiers adds entrypoint-specific Risk Engine alert channels (cmd/desktop's
// Wails App) alongside the structured-log and Slack channels BuildServices always wires.
func WithNotifiers(notifiers ...risk.Notifier) BuildOption {
	return func(s *buildSettings) { s.notifiers = append(s.notifiers, notifiers...) }
}

// WithJevMaxAttempts caps the Jev client's attempts per call; tests use it to
// avoid the production retry backoff. n <= 0 keeps the production policy.
func WithJevMaxAttempts(n int) BuildOption {
	return func(s *buildSettings) { s.jevMaxAttempts = n }
}

// WithKabuBaseURL points the kabuステーションAPI adapter at baseURL instead of
// marketdata.DefaultBaseURL (an httptest server in tests).
func WithKabuBaseURL(baseURL string) BuildOption {
	return func(s *buildSettings) { s.kabuURL = baseURL }
}

// WithYanoshinBaseURL points the default news feed (やのしん TDnet WebAPI) at
// baseURL; tests use it so they never dial the real service.
func WithYanoshinBaseURL(baseURL string) BuildOption {
	return func(s *buildSettings) { s.yanoshinURL = baseURL }
}

// WithNewsClock replaces News Ingest's clock (default time.Now), which its
// 東証立会時間 gate reads; tests pin it inside the session.
func WithNewsClock(now func() time.Time) BuildOption {
	return func(s *buildSettings) { s.newsNow = now }
}

// WithExecutionClock replaces the Execution Engine's clock (default time.Now).
// Tests pin it inside 東証立会時間 so a run during the 昼休み (11:30-12:30
// JST) does not hit the session gate (issue #512).
func WithExecutionClock(now func() time.Time) BuildOption {
	return func(s *buildSettings) { s.executionNow = now }
}

// BuildServices constructs the full composition-root service graph on top
// of state (bootstrap.Run's DB + config) and secrets (config.LoadSecrets),
// registering every internal/service/scheduler queue Handler this build
// wires (market-data, feature-calc, jev-scout, jev-trader,
// outcome-labeling, analytics). opts carry the optional inputs (see
// WithAutoUpdate, WithNotifiers, WithJevMaxAttempts). It performs no I/O
// itself and starts no goroutine; see (*Services).Start.
//
// Each build* step reads the repositories/clients earlier steps stored on
// svc, so the order below is the dependency order.
func BuildServices(state *State, secrets config.Secrets, opts ...BuildOption) *Services {
	var cfg buildSettings
	for _, opt := range opts {
		opt(&cfg)
	}
	alertChannels := alerts.New(secrets)

	svc := &Services{strategy: state.Strategy}
	svc.buildRepositories(state.DB)
	svc.wireActivity()
	svc.buildExternalClients(secrets, alertChannels, cfg)
	svc.buildJevPipeline(state.DB, state.Strategy)
	runtimePolicy := selfimprove.NewRuntimePolicy(svc.Settings, svc.Proposals, state.Strategy.Policy)
	executionConfig := svc.buildRiskAndExecution(state.Risk.Paper, alertChannels, cfg.notifiers, cfg.executionNow, runtimePolicy)
	traderHandler := svc.buildPolicyAndBacktest(state, executionConfig, runtimePolicy)
	svc.buildGovernor(state.Strategy, secrets, alertChannels)
	svc.buildScheduler(state, alertChannels, cfg.autoUpdate)
	marketDataHandler := svc.buildMarketDataPipeline(state.Strategy, state.Paths.LogDir)

	svc.Scheduler.RegisterHandler(jobqueue.JobQueueMarketData, marketDataHandler.HandleMarketData)
	svc.Scheduler.RegisterHandler(jobqueue.JobQueueFeatureCalc, marketdatajob.HandleFeatureCalc)
	svc.Scheduler.RegisterHandler(jobqueue.JobQueueJevScout, svc.Scout.HandleJob)
	svc.Scheduler.RegisterHandler(jobqueue.JobQueueJevTrader, traderHandler.HandleJob)
	svc.Scheduler.RegisterHandler(jobqueue.JobQueueOutcomeLabeling, calibration.NewLabeler(svc.Decisions, svc.Snapshots, svc.Outcomes).HandleJob)
	svc.Scheduler.RegisterHandler(jobqueue.JobQueueAnalytics, svc.handleSelfImprove)

	return svc
}
