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

// BuildServices constructs the full composition-root service graph on top
// of state (bootstrap.Run's DB + config) and secrets (config.LoadSecrets),
// registering every internal/service/scheduler queue Handler this build
// wires (market-data, feature-calc, jev-scout, jev-trader,
// outcome-labeling, analytics). notifiers are entrypoint-specific extra
// Risk Engine alert channels (cmd/desktop's Wails App; cmd/server none),
// and autoUpdate (issue #65) wires internal/service/updater's periodic
// self-update check onto Scheduler when non-nil (cmd/desktop only), also
// exposed as Services.Updater (issue #76). It performs no I/O itself and
// starts no goroutine; see (*Services).Start.
func BuildServices(state *State, secrets config.Secrets, autoUpdate updater.Quitter, notifiers ...risk.Notifier) (*Services, error) {
	instruments := market.NewInstrumentRepository(state.DB)
	snapshots := market.NewSnapshotRepository(state.DB)
	decisions := judgement.NewDecisionRepository(state.DB)
	signals := trading.NewSignalRepository(state.DB)
	jobs := jobqueue.NewJobRepository(state.DB)
	positions := trading.NewPositionRepository(state.DB)
	orders := trading.NewOrderRepository(state.DB)
	outcomes := judgement.NewCalibrationRepository(state.DB)
	killSwitch := system.NewKillSwitchRepository(state.DB)
	settings := system.NewRuntimeSettingsRepository(state.DB)
	alertChannels := alerts.New(secrets)

	// System Activity Log (functional.md §4.15): reads the pipeline's own
	// repositories; their post-commit observers feed `/ws/activity`.
	activity := activityfeed.New(jobs, decisions, killSwitch)
	jobs.SetObserver(activity.ObserveJob)
	decisions.SetObserver(activity.ObserveDecision)
	killSwitch.SetObserver(activity.ObserveKillSwitch)

	ragService := rag.NewService(state.DB, decisions, snapshots)
	featureEngine := featureengine.NewEngine(snapshots, ragService)

	marketDataClient := marketdata.NewClient(marketdata.Config{
		APIPassword: secrets.KabuAPIPassword,
	})

	jevClient := jev.NewClient(jev.Config{
		BaseURL: secrets.JevBaseURL,
		APIKey:  secrets.JevAPIKey,
		Alerts:  alertChannels.JevAlerts(),
		// 0 keeps the production retry policy (jev.defaultMaxAttempts).
		MaxAttempts: jevMaxAttemptsForTest,
	})
	// News Ingest (issue #81, FR-LUNA-1〜5): the external news feed and
	// Luna are both optional secrets; unless both are configured the
	// service still exists (its cache is simply always empty, so no
	// news_context is injected and no news flag is raised) but its
	// polling loop is not started.
	lunaClient := assist.NewClient(assist.Config{Label: "luna", BaseURL: secrets.LunaBaseURL, APIKey: secrets.LunaAPIKey})
	newsFeed := newsfeed.NewFeedClient(newsfeed.FeedConfig{URL: secrets.NewsFeedURL, APIKey: secrets.NewsFeedAPIKey})
	newsEnabled := lunaClient.Configured() && newsFeed.Configured()
	newsService := newsfeed.NewService(newsFeed, assist.NewLuna(lunaClient), instruments)

	scout := jev.NewScout(jevClient, decisions, snapshots, jobs, ragService, state.Strategy.JevScout, jev.WithNewsSource(newsService))
	trader := jev.NewTrader(jevClient, decisions, ragService, jev.WithNewsSource(newsService))

	executionConfig := withTradingCalendar(execution.ConfigFromRiskLimits(state.Risk.Paper))
	executionEngine := execution.NewEngine(execution.Deps{
		Orders:      orders,
		Positions:   positions,
		Snapshots:   snapshots,
		Decisions:   decisions,
		Signals:     signals,
		Instruments: instruments,
	}, executionConfig)

	riskEngine := newRiskEngine(state.Risk.Paper, riskRepositories{
		killSwitch: killSwitch,
		settings:   settings,
		snapshots:  snapshots,
		positions:  positions,
		orders:     orders,
	}, riskSignals{
		marketData: marketDataClient,
		jevAPI:     jevClient,
		brokerAPI:  marketDataClient.BrokerFailures(),
		dbWrite:    sqlitedb.DBWriteFailures,
	}, executionEngine, alertChannels.RiskNotifier(notifiers))
	// runtimePolicy is strategy.yaml's policy.* thresholds overridden by every
	// applied Self-Improvement proposal; signals and backtests both read it, so
	// an approved (or rolled-back) change applies on the next evaluation (#52).
	runtimePolicy := selfimprove.NewRuntimePolicy(settings, state.Strategy.Policy)
	thresholds := policy.ThresholdsFromStrategy(*state.Strategy)
	policyEngine := policy.NewEngine(thresholds, riskEngine, signals, policy.WithPolicySource(runtimePolicy))
	calibrationService := calibration.NewService(outcomes, decisiontrade.New(state.DB))
	traderHandler := policy.NewHandler(trader, snapshots, policyEngine, paperexec.Executor{Engine: executionEngine, Sizer: riskEngine}, policy.WithCalibration(calibrationService))

	backtestSource := backtestsource.New(instruments, snapshots, decisions, thresholds, runtimePolicy, executionConfig)
	// Sol/Opus (issue #82, FR-SELFIMPROVE-8/9) are real external LLM API
	// clients built from the optional SOL_*/OPUS_* secrets. Left unset,
	// each stage is skipped every day (assist.ErrNotConfigured) instead of
	// blocking start-up.
	solClient := assist.NewClient(assist.Config{Label: "sol", BaseURL: secrets.SolBaseURL, APIKey: secrets.SolAPIKey})
	opusClient := assist.NewClient(assist.Config{Label: "opus", BaseURL: secrets.OpusBaseURL, APIKey: secrets.OpusAPIKey})
	proposals := judgement.NewProposalRepository(state.DB)
	governor := selfimprove.NewGovernor(proposals, settings, positions,
		backtestSource, state.Strategy.Policy,
		selfimprove.WithNotifier(alertChannels.SelfImproveNotifier()),
		selfimprove.WithSol(assist.NewSol(solClient)),
		selfimprove.WithOpus(assist.NewOpus(opusClient)))

	schedOpts := []scheduler.Option{
		scheduler.WithOutcomeLabelSource(outcomes),
		scheduler.WithSessionGate(marketcalendarOpen), scheduler.WithHeartbeatChecker(riskEngine),
		scheduler.WithRiskMonitor(riskEngine),
		scheduler.WithAutoResumer(riskEngine),
		scheduler.WithLogRotator(logging.NewArchiver(LogDir, 0)),
		scheduler.WithDataPurger(retention.New(state.DB, retention.Policy{})),
		scheduler.WithMaintenanceState(settings), scheduler.WithMaintenanceNotifier(notify.MaintenanceChannel(alertChannels.Log, alertChannels.Slack)),
	}
	if dir := os.Getenv(EnvBackupDir); dir != "" {
		schedOpts = append(schedOpts, scheduler.WithDatabaseBackuper(backup.New(state.DB, dir, 0)))
	} else {
		slog.Warn("bootstrap: daily database backup disabled: " + EnvBackupDir + " is not set (requirements/non-functional.md §3)")
	}
	var updateAdapter *updater.SchedulerAdapter
	if autoUpdate != nil { // cmd/desktop only (issue #65); cmd/server passes nil
		gate := updater.SafeGate{Positions: repoportfolio.New(positions, state.Risk.Paper.InitialCapital), State: riskEngine, Orders: executionEngine}
		checker := updater.NewChecker(updater.Config{Owner: "ousiassllc", Repo: "pitha-trador", Gate: gate})
		updateAdapter = &updater.SchedulerAdapter{Checker: checker, Quitter: autoUpdate}
		schedOpts = append(schedOpts, scheduler.WithUpdateChecker(updateAdapter))
	}
	sched := scheduler.New(jobs, instruments, schedOpts...)

	screenerSource := screener.NewLiveSource()
	pushFeed := pushfeed.New(instruments, marketDataClient, marketdata.DefaultPushURL, defaultKabuExchange)
	marketDataHandler := &marketdatajob.Handler{
		Boards: pushFeed, Instruments: instruments, Snapshots: snapshots, FeatureEngine: featureEngine,
		Execution: executionEngine, Screener: screenerSource, News: newsService, Scheduler: sched,
		EventTrigger: state.Strategy.Scan.EventTrigger,
	}

	svc := &Services{
		Instruments:   instruments,
		Snapshots:     snapshots,
		Decisions:     decisions,
		Signals:       signals,
		Jobs:          jobs,
		Positions:     positions,
		Orders:        orders,
		Outcomes:      outcomes,
		KillSwitch:    killSwitch,
		Settings:      settings,
		Proposals:     proposals,
		RAG:           ragService,
		MarketData:    marketDataClient,
		FeatureEngine: featureEngine,
		Screener:      screenerSource,
		Jev:           jevClient,
		Scout:         scout,
		Trader:        trader,
		News:          newsService,
		newsEnabled:   newsEnabled,
		Policy:        policyEngine,
		Risk:          riskEngine,
		Execution:     executionEngine,
		Insight:       insight.NewReader(executionEngine, instruments, signals, positions),
		Calibration:   calibrationService,
		Governor:      governor,
		ErrorLogs:     logging.NewExporter(LogDir),
		Backtest:      backtestSource,
		Activity:      activity,
		Scheduler:     sched,
		Updater:       updateAdapter,
		strategy:      state.Strategy,
		PushFeed:      pushFeed,
		candidates: &candidates.Refresher{
			Instruments: instruments, Snapshots: snapshots, Settings: settings, Jobs: jobs,
			Screener: screenerSource, Strategy: state.Strategy, InSession: marketcalendarOpen,
		},
	}

	sched.RegisterHandler(jobqueue.JobQueueMarketData, marketDataHandler.HandleMarketData)
	sched.RegisterHandler(jobqueue.JobQueueFeatureCalc, marketdatajob.HandleFeatureCalc)
	sched.RegisterHandler(jobqueue.JobQueueJevScout, svc.Scout.HandleJob)
	sched.RegisterHandler(jobqueue.JobQueueJevTrader, traderHandler.HandleJob)
	sched.RegisterHandler(jobqueue.JobQueueOutcomeLabeling, calibration.NewLabeler(decisions, snapshots, outcomes).HandleJob)
	sched.RegisterHandler(jobqueue.JobQueueAnalytics, svc.handleSelfImprove)

	return svc, nil
}
