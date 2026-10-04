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
	"database/sql"
	"log/slog"
	"os"
	"sync"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap/alerts"
	"github.com/ousiassllc/pitha-trador/internal/bootstrap/backtestsource"
	"github.com/ousiassllc/pitha-trador/internal/bootstrap/candidates"
	"github.com/ousiassllc/pitha-trador/internal/bootstrap/marketdatajob"
	"github.com/ousiassllc/pitha-trador/internal/bootstrap/paperexec"
	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/logging"
	"github.com/ousiassllc/pitha-trador/internal/repository/decisiontrade"
	"github.com/ousiassllc/pitha-trador/internal/repository/jobqueue"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/repository/trading"
	"github.com/ousiassllc/pitha-trador/internal/service/activityfeed"
	"github.com/ousiassllc/pitha-trador/internal/service/assist"
	"github.com/ousiassllc/pitha-trador/internal/service/backup"
	"github.com/ousiassllc/pitha-trador/internal/service/calibration"
	"github.com/ousiassllc/pitha-trador/internal/service/execution"
	"github.com/ousiassllc/pitha-trador/internal/service/featureengine"
	"github.com/ousiassllc/pitha-trador/internal/service/insight"
	"github.com/ousiassllc/pitha-trador/internal/service/jev"
	"github.com/ousiassllc/pitha-trador/internal/service/marketdata"
	"github.com/ousiassllc/pitha-trador/internal/service/newsfeed"
	"github.com/ousiassllc/pitha-trador/internal/service/notify"
	"github.com/ousiassllc/pitha-trador/internal/service/policy"
	"github.com/ousiassllc/pitha-trador/internal/service/pushfeed"
	"github.com/ousiassllc/pitha-trador/internal/service/rag"
	"github.com/ousiassllc/pitha-trador/internal/service/retention"
	"github.com/ousiassllc/pitha-trador/internal/service/risk"
	"github.com/ousiassllc/pitha-trador/internal/service/risk/repoportfolio"
	"github.com/ousiassllc/pitha-trador/internal/service/scheduler"
	"github.com/ousiassllc/pitha-trador/internal/service/screener"
	"github.com/ousiassllc/pitha-trador/internal/service/selfimprove"
	"github.com/ousiassllc/pitha-trador/internal/service/updater"
	"github.com/ousiassllc/pitha-trador/internal/version"
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
	Outcomes    *judgement.CalibrationRepository
	KillSwitch  *system.KillSwitchRepository
	Settings    *system.RuntimeSettingsRepository
	Proposals   *judgement.ProposalRepository

	RAG           *rag.Service
	MarketData    *marketdata.Client
	PushFeed      *pushfeed.Feed // startup registration + PUSH subscription (flows.md §10.1)
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
	ErrorLogs     *logging.Exporter // read-only export of LogDir (FR-ERRLOG-2)

	Scheduler *scheduler.Scheduler
	Updater   *updater.SchedulerAdapter // nil on cmd/server (issue #76)

	strategy *config.StrategyConfig
	wg       sync.WaitGroup

	newsEnabled bool
	candidates  *candidates.Refresher
}

// buildSettings collects BuildServices' optional inputs; see BuildOption.
type buildSettings struct {
	autoUpdate     updater.Quitter
	notifiers      []risk.Notifier
	jevMaxAttempts int
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
func BuildServices(state *State, secrets config.Secrets, opts ...BuildOption) (*Services, error) {
	var cfg buildSettings
	for _, opt := range opts {
		opt(&cfg)
	}
	alertChannels := alerts.New(secrets)

	svc := &Services{strategy: state.Strategy}
	svc.buildRepositories(state.DB)
	svc.wireActivity()
	svc.buildExternalClients(secrets, alertChannels, cfg.jevMaxAttempts)
	svc.buildJevPipeline(state.DB, state.Strategy)
	executionConfig := svc.buildRiskAndExecution(state.Risk.Paper, alertChannels, cfg.notifiers)
	traderHandler := svc.buildPolicyAndBacktest(state, executionConfig)
	svc.buildGovernor(state.Strategy, secrets, alertChannels)
	svc.buildScheduler(state, alertChannels, cfg.autoUpdate)
	marketDataHandler := svc.buildMarketDataPipeline(state.Strategy)

	svc.Scheduler.RegisterHandler(jobqueue.JobQueueMarketData, marketDataHandler.HandleMarketData)
	svc.Scheduler.RegisterHandler(jobqueue.JobQueueFeatureCalc, marketdatajob.HandleFeatureCalc)
	svc.Scheduler.RegisterHandler(jobqueue.JobQueueJevScout, svc.Scout.HandleJob)
	svc.Scheduler.RegisterHandler(jobqueue.JobQueueJevTrader, traderHandler.HandleJob)
	svc.Scheduler.RegisterHandler(jobqueue.JobQueueOutcomeLabeling, calibration.NewLabeler(svc.Decisions, svc.Snapshots, svc.Outcomes).HandleJob)
	svc.Scheduler.RegisterHandler(jobqueue.JobQueueAnalytics, svc.handleSelfImprove)

	return svc, nil
}

// buildRepositories creates every repository on the shared DB handle.
func (s *Services) buildRepositories(db *sql.DB) {
	s.Instruments = market.NewInstrumentRepository(db)
	s.Snapshots = market.NewSnapshotRepository(db)
	s.Decisions = judgement.NewDecisionRepository(db)
	s.Signals = trading.NewSignalRepository(db)
	s.Jobs = jobqueue.NewJobRepository(db)
	s.Positions = trading.NewPositionRepository(db)
	s.Orders = trading.NewOrderRepository(db)
	s.Outcomes = judgement.NewCalibrationRepository(db)
	s.KillSwitch = system.NewKillSwitchRepository(db)
	s.Settings = system.NewRuntimeSettingsRepository(db)
	s.Proposals = judgement.NewProposalRepository(db)
}

// wireActivity builds the System Activity Log (functional.md §4.15): it reads
// the pipeline's own repositories, whose post-commit observers feed `/ws/activity`.
func (s *Services) wireActivity() {
	s.Activity = activityfeed.New(s.Jobs, s.Decisions, s.KillSwitch)
	s.Jobs.SetObserver(s.Activity.ObserveJob)
	s.Decisions.SetObserver(s.Activity.ObserveDecision)
	s.KillSwitch.SetObserver(s.Activity.ObserveKillSwitch)
}

// buildExternalClients builds the kabuステーション and Jev API clients and the
// News Ingest service (issue #81, FR-LUNA-1〜5). The news feed and Luna are
// both optional secrets: unless both are configured the service still exists
// (its cache is simply always empty, so no news_context is injected and no
// news flag is raised) but its polling loop is not started.
func (s *Services) buildExternalClients(secrets config.Secrets, alertChannels alerts.Channels, jevMaxAttempts int) {
	s.MarketData = marketdata.NewClient(marketdata.Config{
		APIPassword: secrets.KabuAPIPassword,
	})
	s.Jev = jev.NewClient(jev.Config{
		BaseURL: secrets.JevBaseURL,
		Model:   secrets.JevModel,
		APIKey:  secrets.JevAPIKey,
		Alerts:  alertChannels.JevAlerts(),
		// 0 keeps the production retry policy (jev.defaultMaxAttempts).
		MaxAttempts: jevMaxAttempts,
	})

	lunaClient := assist.NewClient(assist.Config{Label: "luna", BaseURL: secrets.LunaBaseURL, APIKey: secrets.LunaAPIKey})
	newsFeed := newsfeed.NewFeedClient(newsfeed.FeedConfig{URL: secrets.NewsFeedURL, APIKey: secrets.NewsFeedAPIKey})
	s.newsEnabled = lunaClient.Configured() && newsFeed.Configured()
	s.News = newsfeed.NewService(newsFeed, assist.NewLuna(lunaClient), s.Instruments)
}

// buildJevPipeline builds RAG, Feature Engine, Screener and the Jev Scout/Trader.
func (s *Services) buildJevPipeline(db *sql.DB, strategy *config.StrategyConfig) {
	s.RAG = rag.NewService(db, s.Decisions, s.Snapshots)
	s.FeatureEngine = featureengine.NewEngine(s.Snapshots, s.RAG)
	s.Screener = screener.NewLiveSource()
	s.Scout = jev.NewScout(s.Jev, s.Decisions, s.Snapshots, s.Jobs, s.RAG, strategy.JevScout, jev.WithNewsSource(s.News), jev.WithScoutRecorder(s.Screener))
	s.Trader = jev.NewTrader(s.Jev, s.Decisions, s.RAG, jev.WithNewsSource(s.News))
}

// buildRiskAndExecution builds the paper Execution Engine and the Risk Engine
// guarding it, and returns the execution config the backtest source reuses.
func (s *Services) buildRiskAndExecution(limits config.RiskLimits, alertChannels alerts.Channels, notifiers []risk.Notifier) execution.Config {
	executionConfig := withTradingCalendar(execution.ConfigFromRiskLimits(limits))
	s.Execution = execution.NewEngine(execution.Deps{
		Orders:      s.Orders,
		Positions:   s.Positions,
		Snapshots:   s.Snapshots,
		Decisions:   s.Decisions,
		Signals:     s.Signals,
		Instruments: s.Instruments,
	}, executionConfig)

	s.Risk = newRiskEngine(limits, riskRepositories{
		killSwitch: s.KillSwitch,
		settings:   s.Settings,
		snapshots:  s.Snapshots,
		positions:  s.Positions,
		orders:     s.Orders,
	}, riskSignals{
		marketData: s.MarketData,
		jevAPI:     s.Jev,
		brokerAPI:  s.MarketData.BrokerFailures(),
		dbWrite:    sqlitedb.DBWriteFailures,
	}, s.Execution, alertChannels.RiskNotifier(notifiers))
	s.Insight = insight.NewReader(s.Execution, s.Instruments, s.Signals, s.Positions)
	return executionConfig
}

// buildPolicyAndBacktest builds the Policy Engine, calibration and backtest
// source, and returns the jev-trader queue Handler. runtimePolicy is
// strategy.yaml's policy.* thresholds overridden by every applied
// Self-Improvement proposal; signals and backtests both read it, so an
// approved (or rolled-back) change applies on the next evaluation (#52).
func (s *Services) buildPolicyAndBacktest(state *State, executionConfig execution.Config) *policy.Handler {
	runtimePolicy := selfimprove.NewRuntimePolicy(s.Settings, state.Strategy.Policy)
	thresholds := policy.ThresholdsFromStrategy(*state.Strategy)
	s.Policy = policy.NewEngine(thresholds, s.Risk, s.Signals, policy.WithPolicySource(runtimePolicy))
	s.Calibration = calibration.NewService(s.Outcomes, decisiontrade.New(state.DB))
	s.Backtest = backtestsource.New(s.Instruments, s.Snapshots, s.Decisions, thresholds, runtimePolicy, executionConfig)
	return policy.NewHandler(s.Trader, s.Snapshots, s.Policy, paperexec.Executor{Engine: s.Execution, Sizer: s.Risk}, policy.WithCalibration(s.Calibration))
}

// buildGovernor builds the Self-Improvement Governor. Sol/Opus (issue #82,
// FR-SELFIMPROVE-8/9) are real external LLM API clients built from the
// optional SOL_*/OPUS_* secrets. Left unset, each stage is skipped every day
// (assist.ErrNotConfigured) instead of blocking start-up.
func (s *Services) buildGovernor(strategy *config.StrategyConfig, secrets config.Secrets, alertChannels alerts.Channels) {
	solClient := assist.NewClient(assist.Config{Label: "sol", BaseURL: secrets.SolBaseURL, APIKey: secrets.SolAPIKey})
	opusClient := assist.NewClient(assist.Config{Label: "opus", BaseURL: secrets.OpusBaseURL, APIKey: secrets.OpusAPIKey})
	s.Governor = selfimprove.NewGovernor(s.Proposals, s.Settings, s.Positions,
		s.Backtest, strategy.Policy,
		selfimprove.WithNotifier(alertChannels.SelfImproveNotifier()),
		selfimprove.WithSol(assist.NewSol(solClient)),
		selfimprove.WithOpus(assist.NewOpus(opusClient)))
}

// buildScheduler builds the Scheduler with its maintenance tasks and, when
// autoUpdate is non-nil (cmd/desktop only, issue #65), the self-update check.
func (s *Services) buildScheduler(state *State, alertChannels alerts.Channels, autoUpdate updater.Quitter) {
	schedOpts := []scheduler.Option{
		scheduler.WithOutcomeLabelSource(s.Outcomes),
		scheduler.WithSessionGate(marketcalendarOpen), scheduler.WithHeartbeatChecker(s.Risk),
		scheduler.WithRiskMonitor(s.Risk),
		scheduler.WithAutoResumer(s.Risk),
		scheduler.WithLogRotator(logging.NewArchiver(LogDir, 0)),
		scheduler.WithDataPurger(retention.New(state.DB, retention.Policy{})),
		scheduler.WithMaintenanceState(s.Settings), scheduler.WithMaintenanceNotifier(notify.MaintenanceChannel(alertChannels.Log, alertChannels.Slack)),
	}
	if dir := os.Getenv(EnvBackupDir); dir != "" {
		schedOpts = append(schedOpts, scheduler.WithDatabaseBackuper(backup.New(state.DB, dir, 0)))
	} else {
		slog.Warn("bootstrap: daily database backup disabled: " + EnvBackupDir + " is not set (requirements/non-functional.md §3)")
	}
	if autoUpdate != nil {
		gate := updater.SafeGate{Positions: repoportfolio.New(s.Positions, state.Risk.Paper.InitialCapital), State: s.Risk, Orders: s.Execution}
		checker := updater.NewChecker(updater.Config{Owner: version.GitHubOwner, Repo: version.GitHubRepo, Gate: gate})
		s.Updater = &updater.SchedulerAdapter{Checker: checker, Quitter: autoUpdate}
		schedOpts = append(schedOpts, scheduler.WithUpdateChecker(s.Updater))
	}
	s.Scheduler = scheduler.New(s.Jobs, s.Instruments, schedOpts...)
}

// buildMarketDataPipeline builds the PUSH feed, the Scanner Dashboard
// candidate refresher and the log exporter, and returns the market-data
// queue Handler wired onto them.
func (s *Services) buildMarketDataPipeline(strategy *config.StrategyConfig) *marketdatajob.Handler {
	s.PushFeed = pushfeed.New(s.Instruments, s.MarketData, marketdata.DefaultPushURL, defaultKabuExchange)
	s.ErrorLogs = logging.NewExporter(LogDir)
	s.candidates = &candidates.Refresher{
		Instruments: s.Instruments, Snapshots: s.Snapshots, Settings: s.Settings, Jobs: s.Jobs,
		Screener: s.Screener, Strategy: strategy, InSession: marketcalendarOpen,
	}
	return &marketdatajob.Handler{
		Boards: s.PushFeed, Instruments: s.Instruments, Snapshots: s.Snapshots, FeatureEngine: s.FeatureEngine,
		Execution: s.Execution, Screener: s.Screener, News: s.News, Scheduler: s.Scheduler,
		EventTrigger: strategy.Scan.EventTrigger,
	}
}
