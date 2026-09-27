// Package bootstrap's services.go (extended incrementally by issue #41's
// other split children, #44 onward) is the composition root that builds
// every internal/service/* instance cmd/desktop and cmd/server share, and
// registers each internal/service/scheduler queue's Handler. BuildServices
// only *constructs* everything; nothing in it runs a background goroutine
// (kabuステーションAPI token refresh, Scheduler workers/cron) until
// cmd/desktop's Wails OnStartup or cmd/server's main calls
// (*Services).Start (lifecycle.go). Queue Handlers live in
// marketdata_job.go; the Scanner Dashboard candidate-refresh cycle lives
// in candidates.go.
package bootstrap

import (
	"sync"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/logging"
	"github.com/ousiassllc/pitha-trador/internal/repository"
	"github.com/ousiassllc/pitha-trador/internal/service/calibration"
	"github.com/ousiassllc/pitha-trador/internal/service/execution"
	"github.com/ousiassllc/pitha-trador/internal/service/featureengine"
	"github.com/ousiassllc/pitha-trador/internal/service/jev"
	"github.com/ousiassllc/pitha-trador/internal/service/marketdata"
	"github.com/ousiassllc/pitha-trador/internal/service/policy"
	"github.com/ousiassllc/pitha-trador/internal/service/rag"
	"github.com/ousiassllc/pitha-trador/internal/service/risk"
	"github.com/ousiassllc/pitha-trador/internal/service/scheduler"
	"github.com/ousiassllc/pitha-trador/internal/service/screener"
	"github.com/ousiassllc/pitha-trador/internal/service/selfimprove"
)

// LogDir is the directory every entrypoint's logging.RotatingWriter
// writes the daily structured JSON log file to, and the Scheduler's
// @daily logging.Archiver compresses files past their 30-day retention in
// (requirements/non-functional.md §5).
const LogDir = "logs"

// defaultTokenRefreshInterval is how often Services.Start reissues the
// kabuステーションAPI token (marketdata.Client.Start). kabuステーション
// API's own reference does not publish an exact token TTL
// (docs/architecture/overview.md §5 only says "有効期限があるため...
// 定期的に再発行"); 20 minutes is a conservative guess that comfortably
// reissues well before any plausible expiry while staying well above
// fullScanInterval (60s) so it never dominates request volume.
const defaultTokenRefreshInterval = 20 * time.Minute

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
	Signals     *repository.SignalRepository
	Jobs        *repository.JobRepository
	Positions   *repository.PositionRepository
	Orders      *repository.OrderRepository
	Outcomes    *repository.CalibrationRepository
	KillSwitch  *repository.KillSwitchRepository
	Settings    *repository.RuntimeSettingsRepository

	RAG           *rag.Service
	MarketData    *marketdata.Client
	FeatureEngine *featureengine.Engine
	Screener      *screener.LiveSource
	Jev           *jev.Client
	Scout         *jev.Scout
	Trader        *jev.Trader
	Policy        *policy.Engine
	Risk          *risk.Engine
	Execution     *execution.Engine
	Calibration   *calibration.Service
	Backtest      *BacktestSource
	Governor      *selfimprove.Governor

	Scheduler *scheduler.Scheduler

	strategy *config.StrategyConfig
	wg       sync.WaitGroup
}

// BuildServices constructs the full composition-root service graph on top
// of state (bootstrap.Run's DB + config) and secrets (config.LoadSecrets),
// registering every internal/service/scheduler queue Handler this build
// wires (market-data, feature-calc, jev-scout, jev-trader,
// outcome-labeling, analytics). notifiers are
// entrypoint-specific extra Risk Engine alert channels (cmd/desktop passes
// its Wails App for native OS toasts; cmd/server passes none) fanned out
// alongside the always-on structured log and optional Slack channels
// (risk.go). It performs no I/O itself (no DB queries beyond what the
// repository constructors below do, which is none - they only hold
// *sql.DB) and starts no goroutine; see (*Services).Start.
func BuildServices(state *State, secrets config.Secrets, notifiers ...risk.Notifier) (*Services, error) {
	instruments := repository.NewInstrumentRepository(state.DB)
	snapshots := repository.NewSnapshotRepository(state.DB)
	decisions := repository.NewDecisionRepository(state.DB)
	signals := repository.NewSignalRepository(state.DB)
	jobs := repository.NewJobRepository(state.DB)
	positions := repository.NewPositionRepository(state.DB)
	orders := repository.NewOrderRepository(state.DB)
	outcomes := repository.NewCalibrationRepository(state.DB)
	killSwitch := repository.NewKillSwitchRepository(state.DB)
	settings := repository.NewRuntimeSettingsRepository(state.DB)
	alerts := newAlertChannels(secrets)

	ragService := rag.NewService(state.DB, decisions, snapshots)
	featureEngine := featureengine.NewEngine(snapshots, ragService)

	marketDataClient := marketdata.NewClient(marketdata.Config{
		APIPassword: secrets.KabuAPIPassword,
	})

	jevClient := jev.NewClient(jev.Config{
		BaseURL: secrets.JevBaseURL,
		APIKey:  secrets.JevAPIKey,
		Alerts:  alerts.jevAlerts(),
	})
	scout := jev.NewScout(jevClient, decisions, snapshots, jobs, ragService, state.Strategy.JevScout)
	trader := jev.NewTrader(jevClient, decisions, ragService)

	executionConfig := execution.ConfigFromRiskLimits(state.Risk.Paper)
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
	}, executionEngine, alerts.riskNotifier(notifiers))
	// runtimePolicy is config/strategy.yaml's policy.* thresholds as
	// overridden by every Self-Improvement proposal currently applied:
	// live signals and backtests both read it, so an approved (or
	// rolled-back) change takes effect on the next evaluation (#52).
	runtimePolicy := selfimprove.NewRuntimePolicy(settings, state.Strategy.Policy)
	thresholds := policy.ThresholdsFromStrategy(*state.Strategy)
	policyEngine := policy.NewEngine(thresholds, riskEngine, signals, policy.WithPolicySource(runtimePolicy))
	traderHandler := policy.NewHandler(trader, snapshots, policyEngine, paperExecutor{engine: executionEngine})

	backtestSource := newBacktestSource(instruments, snapshots, decisions, thresholds, runtimePolicy, executionConfig)
	calibrationService := calibration.NewService(outcomes)
	governor := selfimprove.NewGovernor(repository.NewProposalRepository(state.DB), settings, positions,
		backtestSource, state.Strategy.Policy, selfimprove.WithNotifier(alerts.selfImproveNotifier()))

	sched := scheduler.New(jobs, instruments,
		scheduler.WithOutcomeLabelSource(outcomes),
		scheduler.WithHeartbeatChecker(riskEngine),
		scheduler.WithLogRotator(logging.NewArchiver(LogDir, 0)),
	)

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
		RAG:           ragService,
		MarketData:    marketDataClient,
		FeatureEngine: featureEngine,
		Screener:      screener.NewLiveSource(),
		Jev:           jevClient,
		Scout:         scout,
		Trader:        trader,
		Policy:        policyEngine,
		Risk:          riskEngine,
		Execution:     executionEngine,
		Calibration:   calibrationService,
		Governor:      governor,
		Backtest:      backtestSource,
		Scheduler:     sched,
		strategy:      state.Strategy,
	}

	sched.RegisterHandler(repository.JobQueueMarketData, svc.handleMarketData)
	sched.RegisterHandler(repository.JobQueueFeatureCalc, svc.handleFeatureCalc)
	sched.RegisterHandler(repository.JobQueueJevScout, svc.Scout.HandleJob)
	sched.RegisterHandler(repository.JobQueueJevTrader, traderHandler.HandleJob)
	sched.RegisterHandler(repository.JobQueueOutcomeLabeling, calibration.NewLabeler(decisions, snapshots, outcomes).HandleJob)
	sched.RegisterHandler(repository.JobQueueAnalytics, svc.handleSelfImprove)

	return svc, nil
}
