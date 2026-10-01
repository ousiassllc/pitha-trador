package router

import (
	"time"

	"github.com/ousiassllc/pitha-trador/internal/web/handler"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/activity"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/settings"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/symbol"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/system"
	"github.com/ousiassllc/pitha-trador/internal/web/insightapi"
	"github.com/ousiassllc/pitha-trador/internal/web/middleware"
)

// defaultCandidateRefreshInterval mirrors config/strategy.yaml's
// scan.candidate_refresh_interval_seconds_min/max defaults
// (functional.md §4.3, §5.1 "候補銘柄更新周期（15〜30秒）").
var defaultCandidateRefreshInterval = handler.CandidateRefreshInterval{
	Min: 15 * time.Second,
	Max: 30 * time.Second,
}

type options struct {
	candidateSource   handler.CandidateSource
	candidateRefresh  handler.CandidateRefreshInterval
	systemEngine      system.SystemEngine
	symbolProvider    symbol.SymbolProvider
	symbolRiskParams  symbol.SymbolRiskParams
	insightProvider   insightapi.Provider
	calibrationSource handler.CalibrationSource
	proposalSource    handler.PolicyProposalSource
	backtestRunner    handler.BacktestRunner
	activitySource    activity.ActivitySource
	secretsStore      settings.SecretsStore // nil until WithSecretsStore; also gates the Setup Guard
	updateController  system.UpdateController
	errorLogExporter  system.ErrorLogExporter
	heartbeatRecorder middleware.HeartbeatRecorder // nil until WithHeartbeatRecorder: no heartbeat recording
	allowedHosts      []string                     // nil until WithAllowedHosts: no Host/Origin validation
	wsBase            string                       // "" until WithWebSocketBase: WebSockets use the page's own origin
}

// Option configures New.
type Option func(*options)

// WithCandidateSource overrides the Scanner Dashboard/API/WebSocket data
// source (internal/web/handler.CandidateSource). Defaults to an empty
// handler.StaticCandidateSource until a later sub-scope wires the
// Scheduler's live Fast Screener output in.
func WithCandidateSource(source handler.CandidateSource) Option {
	return func(o *options) { o.candidateSource = source }
}

// WithCandidateRefreshInterval overrides the `/ws/scanner` push spacing.
// Defaults to defaultCandidateRefreshInterval (15-30s).
func WithCandidateRefreshInterval(interval handler.CandidateRefreshInterval) Option {
	return func(o *options) { o.candidateRefresh = interval }
}

// WithSystemEngine overrides the Kill Switch action/API routes' backing
// internal/web/handler/system.SystemEngine. cmd/desktop and cmd/server pass
// internal/bootstrap's real internal/service/risk.Engine; the default
// Running system.StaticSystemEngine only serves router-level tests.
func WithSystemEngine(engine system.SystemEngine) Option {
	return func(o *options) { o.systemEngine = engine }
}

// WithSymbolProvider overrides the Symbol Detail/position/order routes'
// backing internal/web/handler/symbol.SymbolProvider. cmd/desktop and
// cmd/server pass internal/bootstrap's real
// internal/service/execution.Engine; the empty symbol.StaticSymbolProvider
// default only serves router-level tests.
func WithSymbolProvider(provider symbol.SymbolProvider) Option {
	return func(o *options) { o.symbolProvider = provider }
}

// WithSymbolRiskParams overrides `GET /api/v1/symbols/{symbol}`'s "risk"
// section (symbol.SymbolRiskParams). cmd/* build it from the real
// risk.Engine/execution.Engine settings via symbol.NewSymbolRiskParams;
// without this option the section reports zero values.
func WithSymbolRiskParams(params symbol.SymbolRiskParams) Option {
	return func(o *options) { o.symbolRiskParams = params }
}

// WithInsightProvider overrides the decisions/signals/performance API
// routes' backing internal/web/insightapi.Provider. cmd/desktop and
// cmd/server pass internal/bootstrap's real internal/service/insight.Reader;
// the empty insightapi.StaticProvider default only serves router-level
// tests.
func WithInsightProvider(provider insightapi.Provider) Option {
	return func(o *options) { o.insightProvider = provider }
}

// WithCalibrationSource overrides `GET /api/v1/calibration`'s backing
// internal/web/handler.CalibrationSource. cmd/desktop and cmd/server pass
// internal/bootstrap's real internal/service/calibration.Service; the
// empty handler.StaticCalibrationSource default only serves router-level
// tests.
func WithCalibrationSource(source handler.CalibrationSource) Option {
	return func(o *options) { o.calibrationSource = source }
}

// WithPolicyProposalSource overrides `GET /api/v1/policy-proposals`'s
// backing internal/web/handler.PolicyProposalSource. cmd/desktop and
// cmd/server pass internal/bootstrap's *judgement.ProposalRepository; the
// empty handler.StaticPolicyProposalSource default only serves
// router-level tests.
func WithPolicyProposalSource(source handler.PolicyProposalSource) Option {
	return func(o *options) { o.proposalSource = source }
}

// WithActivitySource overrides System Activity Log's backing
// internal/web/handler/activity.ActivitySource (`GET /activity`,
// `GET /api/v1/activity`, `/ws/activity`). cmd/desktop and cmd/server pass
// internal/bootstrap's internal/service/activityfeed.Service; the idle
// activity.StaticActivitySource default only serves router-level tests.
func WithActivitySource(source activity.ActivitySource) Option {
	return func(o *options) { o.activitySource = source }
}

// WithBacktestRunner overrides `GET /performance`'s backing
// internal/web/handler.BacktestRunner. cmd/desktop and cmd/server pass
// internal/bootstrap/backtestsource's Source; the empty
// handler.StaticBacktestRunner default only serves router-level tests.
func WithBacktestRunner(runner handler.BacktestRunner) Option {
	return func(o *options) { o.backtestRunner = runner }
}

// WithSecretsStore sets the Settings/Setup screens' and secrets-status
// banner's backing internal/web/handler/settings.SecretsStore, and enables the
// Setup Guard (middleware.SetupGuard, issue #80): while any required key
// is unset in store, every route except `/setup`, `POST`/`DELETE
// /settings/:key` and `/static/...` redirects to `/setup`. cmd/desktop
// and cmd/server pass internal/bootstrap's real
// *system.SecretsRepository. Without this option (router-level tests
// only) the handlers use the empty settings.StaticSecretsStore and no
// guard is installed, so unrelated route tests need not seed secrets.
func WithSecretsStore(store settings.SecretsStore) Option {
	return func(o *options) { o.secretsStore = store }
}

// WithUpdateController enables the update notification routes (`GET
// /system/update-status`, `GET /system/update-panel`, `POST
// /system/update-check`, issue #76) backed by controller. cmd/desktop
// passes internal/bootstrap's updater.SchedulerAdapter; without it (cmd/
// server, which never self-updates) the two GET routes render nothing and
// the POST route 404s.
func WithUpdateController(controller system.UpdateController) Option {
	return func(o *options) { o.updateController = controller }
}

// WithErrorLogExporter overrides `GET /api/v1/logs/errors`'s backing
// system.ErrorLogExporter (FR-ERRLOG-2). cmd/desktop and cmd/server pass
// internal/bootstrap's *logging.Exporter over the log directory; the empty
// system.StaticErrorLogExporter default only serves router-level tests.
func WithErrorLogExporter(exporter system.ErrorLogExporter) Option {
	return func(o *options) { o.errorLogExporter = exporter }
}

// WithHeartbeatRecorder enables operator heartbeat recording (FR-RISK-6,
// middleware.Heartbeat): every authenticated UI request (session cookie
// present; `/static`, WebSocket upgrades and background timer polls
// excepted) calls recorder.RecordHeartbeat. cmd/desktop and cmd/server
// pass internal/bootstrap's real internal/service/risk.Engine; without it
// (router-level tests only) no heartbeat is written. Live's dead-man's
// switch (Engine.CheckHeartbeatTimeout, run every minute by the Scheduler)
// would fire spuriously if a Live build ran without it.
func WithHeartbeatRecorder(recorder middleware.HeartbeatRecorder) Option {
	return func(o *options) { o.heartbeatRecorder = recorder }
}

// WithAllowedHosts installs middleware.HostGuard with hosts: every request
// whose Host header (and, on state-changing requests and WebSocket
// upgrades, Origin header) names another host is answered 403 before
// Session sees it, defeating DNS rebinding (issue #136). cmd/server passes
// its loopback/configured hosts, cmd/desktop middleware.WailsHosts();
// without it (router-level tests only, whose httptest requests carry
// `example.com`) nothing is validated.
func WithAllowedHosts(hosts ...string) Option {
	return func(o *options) { o.allowedHosts = append([]string{}, hosts...) } // non-nil even for zero hosts: reject everything
}

// WithWebSocketBase makes every full page tell its Lit components to open
// WebSockets at base (e.g. `ws://wails.localhost:51234`) instead of the
// page's own origin. cmd/desktop passes its WebSocket-only loopback
// listener's address, because the Wails AssetServer cannot carry WebSockets
// (issue #266); cmd/server leaves it unset.
func WithWebSocketBase(base string) Option {
	return func(o *options) { o.wsBase = base }
}
