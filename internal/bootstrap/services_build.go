package bootstrap

// services_build.go holds the build* steps BuildServices (services.go) runs in
// dependency order; each stores what it constructs on the *Services receiver.

import (
	"database/sql"
	"log/slog"
	"os"

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
	runtimePolicy := selfimprove.NewRuntimePolicy(s.Settings, s.Proposals, state.Strategy.Policy)
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
