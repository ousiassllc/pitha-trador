package router

import (
	"time"

	"github.com/ousiassllc/pitha-trador/internal/web/handler/activity"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/calibration"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/performance"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/proposals"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/scanner"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/settings"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/symbol"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/system"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/watchlist"
	"github.com/ousiassllc/pitha-trador/internal/web/insightapi"
	"github.com/ousiassllc/pitha-trador/internal/web/middleware"
)

// defaultCandidateRefreshInterval mirrors config/strategy.yaml's
// scan.candidate_refresh_interval_seconds_min/max defaults
// (functional.md §4.3, §5.1 "候補銘柄更新周期（15〜30秒）").
var defaultCandidateRefreshInterval = scanner.CandidateRefreshInterval{
	Min: 15 * time.Second,
	Max: 30 * time.Second,
}

type options struct {
	candidateSource     scanner.CandidateSource
	candidateRefresh    scanner.CandidateRefreshInterval
	universeImporter    scanner.UniverseImporter
	systemEngine        system.SystemEngine
	symbolProvider      symbol.SymbolProvider
	symbolRiskParams    symbol.SymbolRiskParams
	insightProvider     insightapi.Provider
	calibrationSource   calibration.CalibrationSource
	proposalSource      proposals.PolicyProposalSource
	backtestRunner      performance.BacktestRunner
	activitySource      activity.ActivitySource
	watchlistSource     watchlist.Source
	secretsStore        settings.SecretsStore        // nil until WithSecretsStore; also gates the Setup Guard
	operationalSettings settings.OperationalSettings // nil until WithOperationalSettings: no 運用設定 section
	brokerSession       settings.BrokerSession       // nil until WithBrokerSession: no 立花 session state
	updateController    system.UpdateController
	marketDataStatus    system.MarketDataStatusSource
	errorLogExporter    system.ErrorLogExporter
	heartbeatRecorder   middleware.HeartbeatRecorder // nil until WithHeartbeatRecorder: no heartbeat recording
	allowedHosts        []string                     // nil until WithAllowedHosts: no Host/Origin validation
	wsBase              string                       // "" until WithWebSocketBase: WebSockets use the page's own origin
}

// Option configures New.
type Option func(*options)

// WithCandidateSource overrides the Scanner Dashboard/API/WebSocket data
// source (internal/web/handler/scanner.CandidateSource). Defaults to an empty
// scanner.StaticCandidateSource until a later sub-scope wires the
// Scheduler's live Fast Screener output in.
func WithCandidateSource(source scanner.CandidateSource) Option {
	return func(o *options) { o.candidateSource = source }
}

// WithCandidateRefreshInterval overrides the `/ws/scanner` push spacing.
// Defaults to defaultCandidateRefreshInterval (15-30s).
func WithCandidateRefreshInterval(interval scanner.CandidateRefreshInterval) Option {
	return func(o *options) { o.candidateRefresh = interval }
}

// WithUniverseImporter enables the Scanner Dashboard's 銘柄マスタ未投入
// guidance and operator-confirmed JPX import (issue #508,
// internal/web/handler/scanner.UniverseImporter). Without it the panel keeps
// its plain empty state and `POST /scanner/universe/import` 404s.
func WithUniverseImporter(imp scanner.UniverseImporter) Option {
	return func(o *options) { o.universeImporter = imp }
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
// internal/web/handler/calibration.CalibrationSource. cmd/desktop and cmd/server pass
// internal/bootstrap's real internal/service/calibration.Service; the
// empty calibration.StaticCalibrationSource default only serves router-level
// tests.
func WithCalibrationSource(source calibration.CalibrationSource) Option {
	return func(o *options) { o.calibrationSource = source }
}

// WithPolicyProposalSource overrides `GET /api/v1/policy-proposals`'s
// backing internal/web/handler/proposals.PolicyProposalSource. cmd/desktop and
// cmd/server pass internal/bootstrap's *judgement.ProposalRepository; the
// empty proposals.StaticPolicyProposalSource default only serves
// router-level tests.
func WithPolicyProposalSource(source proposals.PolicyProposalSource) Option {
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

// WithWatchlistSource overrides `GET /watchlist`'s backing
// internal/web/handler/watchlist.Source. cmd/desktop and cmd/server pass
// internal/bootstrap's internal/bootstrap/tachibanawatch.Viewer when 立花 is
// selected; the idle watchlist.StaticSource default (kabu selected, router-level
// tests) renders the "not maintained" notice.
func WithWatchlistSource(source watchlist.Source) Option {
	return func(o *options) { o.watchlistSource = source }
}

// WithBacktestRunner overrides `GET /performance`'s backing
// internal/web/handler/performance.BacktestRunner. cmd/desktop and cmd/server pass
// internal/bootstrap/backtestsource's Source; the empty
// performance.StaticBacktestRunner default only serves router-level tests.
func WithBacktestRunner(runner performance.BacktestRunner) Option {
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

// WithOperationalSettings enables the Settings screen's 運用設定 section
// and its `POST`/`DELETE /ops-settings/:key` routes, backed by ops (issue
// #708); without it the section is hidden and the routes answer 404.
func WithOperationalSettings(ops settings.OperationalSettings) Option {
	return func(o *options) { o.operationalSettings = ops }
}

// WithBrokerSession lets the Settings screen's 立花証券 e支店 card show the
// running broker adapter's session state (issue #738); the card shows it
// only while that adapter is 立花.
func WithBrokerSession(session settings.BrokerSession) Option {
	return func(o *options) { o.brokerSession = session }
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

// WithMarketDataStatus enables `GET /system/marketdata-status` (issues #295,
// #739), the header banner telling the operator why the broker session
// (kabu: the kabuステーションAPI token; 立花: the e支店 login) is failing.
// cmd/desktop and cmd/server pass internal/bootstrap's broker.Broker;
// without it the route renders nothing. With WithOperationalSettings the
// banner follows the broker selection (立花: per-cause guidance and the
// demo / production environment).
func WithMarketDataStatus(source system.MarketDataStatusSource) Option {
	return func(o *options) { o.marketDataStatus = source }
}

// WithErrorLogExporter overrides `GET /api/v1/logs/errors`'s backing
// system.ErrorLogExporter (FR-ERRLOG-2). cmd/desktop and cmd/server pass
// internal/bootstrap's *logging.Exporter over the log directory. Without it
// the route answers 500 (system.UnconfiguredErrorLogExporter) so a missing
// wiring is noticed rather than served as an empty log.
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
