// Package router builds the single Gin engine shared by cmd/desktop (via
// Wails' options.App.AssetServer.Handler) and cmd/server (via net/http). It
// intentionally has no dependency on Wails so that a headless server can be
// run without a Wails process (see docs/architecture/overview.md §9).
package router

import (
	"net/http"
	"os"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humagin"
	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/web/handler"
	"github.com/ousiassllc/pitha-trador/internal/web/insightapi"
	"github.com/ousiassllc/pitha-trador/internal/web/middleware"
	staticassets "github.com/ousiassllc/pitha-trador/static/src"
)

// EnvStaticDir names the env var that, when set to an existing directory,
// makes `/static/...` serve straight from disk instead of the embedded
// staticassets.FS snapshot below. `make dev` sets this to static/src so
// `bun --cwd static run dev`'s esbuild/Tailwind watch rebuilds are visible
// on the next page reload with no Go rebuild required. `wails dev`'s
// built-in file watcher only rebuilds/relaunches the Go binary on changes
// to files with a `.go` extension by default (Wails "Application
// Development" guide), so it does not react to static/src/dist's .js/.css
// output changing - an embed-only static handler would keep serving a
// stale compile-time snapshot for the rest of the `make dev` session
// without this override.
const EnvStaticDir = "PITHA_STATIC_DIR"

// staticFS serves `/static/...`. It falls back to staticassets.FS - dist/
// (esbuild/Tailwind output, components/overview.md §2) and vendor/
// (htmx.min.js) - embedded at compile time, so the same bytes ship inside a
// packaged `wails build`/`go build ./cmd/server` .exe regardless of the
// process's cwd or the source tree's location (architecture/overview.md
// §7). See EnvStaticDir above for the dev-mode disk-backed override this
// embedded snapshot is a fallback from.
func staticFS() http.FileSystem {
	if dir := os.Getenv(EnvStaticDir); dir != "" {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return http.Dir(dir)
		}
	}
	return http.FS(staticassets.FS)
}

// defaultCandidateRefreshInterval mirrors config/strategy.yaml's
// scan.candidate_refresh_interval_seconds_min/max defaults
// (functional.md §4.3, §5.1 "候補銘柄更新周期（15〜30秒）").
var defaultCandidateRefreshInterval = handler.CandidateRefreshInterval{
	Min: 15 * time.Second,
	Max: 30 * time.Second,
}

// defaultSymbolRiskParams mirrors config/risk.yaml's Paper
// max_position_per_symbol_pct (2.0) plus FR-EXIT-2's initial
// stop_loss_pct/take_profit_pct (0.6/1.2).
var defaultSymbolRiskParams = handler.SymbolRiskParams{
	AllowedPositionPct: 2.0,
	StopLossPct:        0.6,
	TakeProfitPct:      1.2,
}

type options struct {
	candidateSource   handler.CandidateSource
	candidateRefresh  handler.CandidateRefreshInterval
	systemEngine      handler.SystemEngine
	symbolProvider    handler.SymbolProvider
	symbolRiskParams  handler.SymbolRiskParams
	insightProvider   insightapi.Provider
	calibrationSource handler.CalibrationSource
	proposalSource    handler.PolicyProposalSource
	backtestRunner    handler.BacktestRunner
	activitySource    handler.ActivitySource
	secretsStore      handler.SecretsStore // nil until WithSecretsStore; also gates the Setup Guard
	updateController  handler.UpdateController
	heartbeatRecorder middleware.HeartbeatRecorder // nil until WithHeartbeatRecorder: no heartbeat recording
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
// internal/web/handler.SystemEngine. cmd/desktop and cmd/server pass
// internal/bootstrap's real internal/service/risk.Engine; the default
// Running handler.StaticSystemEngine only serves router-level tests.
func WithSystemEngine(engine handler.SystemEngine) Option {
	return func(o *options) { o.systemEngine = engine }
}

// WithSymbolProvider overrides the Symbol Detail/position/order routes'
// backing internal/web/handler.SymbolProvider. cmd/desktop and
// cmd/server pass internal/bootstrap's real
// internal/service/execution.Engine; the empty handler.StaticSymbolProvider
// default only serves router-level tests.
func WithSymbolProvider(provider handler.SymbolProvider) Option {
	return func(o *options) { o.symbolProvider = provider }
}

// WithSymbolRiskParams overrides `GET /api/v1/symbols/{symbol}`'s "risk"
// section (handler.SymbolRiskParams). Defaults to FR-EXIT-2's initial
// values (stop_loss_pct=0.6, take_profit_pct=1.2) plus config/risk.yaml's
// Paper max_position_per_symbol_pct (2.0).
func WithSymbolRiskParams(params handler.SymbolRiskParams) Option {
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
// cmd/server pass internal/bootstrap's *repository.ProposalRepository; the
// empty handler.StaticPolicyProposalSource default only serves
// router-level tests.
func WithPolicyProposalSource(source handler.PolicyProposalSource) Option {
	return func(o *options) { o.proposalSource = source }
}

// WithActivitySource overrides System Activity Log's backing
// internal/web/handler.ActivitySource (`GET /activity`,
// `GET /api/v1/activity`, `/ws/activity`). cmd/desktop and cmd/server pass
// internal/bootstrap's internal/service/activityfeed.Service; the idle
// handler.StaticActivitySource default only serves router-level tests.
func WithActivitySource(source handler.ActivitySource) Option {
	return func(o *options) { o.activitySource = source }
}

// WithBacktestRunner overrides `GET /performance`'s backing
// internal/web/handler.BacktestRunner. cmd/desktop and cmd/server pass
// internal/bootstrap's BacktestSource; the empty
// handler.StaticBacktestRunner default only serves router-level tests.
func WithBacktestRunner(runner handler.BacktestRunner) Option {
	return func(o *options) { o.backtestRunner = runner }
}

// WithSecretsStore sets the Settings/Setup screens' and secrets-status
// banner's backing internal/web/handler.SecretsStore, and enables the
// Setup Guard (middleware.SetupGuard, issue #80): while any required key
// is unset in store, every route except `/setup`, `POST`/`DELETE
// /settings/:key` and `/static/...` redirects to `/setup`. cmd/desktop
// and cmd/server pass internal/bootstrap's real
// *repository.SecretsRepository. Without this option (router-level tests
// only) the handlers use the empty handler.StaticSecretsStore and no
// guard is installed, so unrelated route tests need not seed secrets.
func WithSecretsStore(store handler.SecretsStore) Option {
	return func(o *options) { o.secretsStore = store }
}

// WithUpdateController enables the update notification routes (`GET
// /system/update-status`, `GET /system/update-panel`, `POST
// /system/update-check`, issue #76) backed by controller. cmd/desktop
// passes internal/bootstrap's updater.SchedulerAdapter; without it (cmd/
// server, which never self-updates) the two GET routes render nothing and
// the POST route 404s.
func WithUpdateController(controller handler.UpdateController) Option {
	return func(o *options) { o.updateController = controller }
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

// New builds and returns the shared Gin engine: the placeholder root
// page, the `/swagger` API docs UI, the Huma-based `/api/v1` JSON API, and
// the Scanner Dashboard SSR/WebSocket routes (docs/api/endpoints.md).
// Route registration otherwise stays minimal at this stage; later
// sub-scopes register the remaining SSR routes (Templ/HTMX, via
// internal/web/handler) on top of this engine.
func New(opts ...Option) *gin.Engine {
	o := options{
		candidateSource:   handler.StaticCandidateSource{},
		candidateRefresh:  defaultCandidateRefreshInterval,
		systemEngine:      handler.StaticSystemEngine{},
		symbolProvider:    handler.StaticSymbolProvider{},
		insightProvider:   insightapi.StaticProvider{},
		symbolRiskParams:  defaultSymbolRiskParams,
		calibrationSource: handler.StaticCalibrationSource{},
		proposalSource:    handler.StaticPolicyProposalSource{},
		backtestRunner:    handler.StaticBacktestRunner{},
		activitySource:    handler.StaticActivitySource{},
	}
	for _, opt := range opts {
		opt(&o)
	}

	engine := gin.New()
	// Registered first, before SetupGuard, so it covers every route
	// (`/static` excepted inside): the session cookie + CSRF token are
	// required for all state-changing methods and WebSocket upgrades, and
	// the Setup screen's `POST`/`DELETE /settings/:key` (which SetupGuard
	// lets through unauthenticated) are protected too (issues #90/#98/#99).
	engine.Use(middleware.NewSession().Handler())
	// After Session (needs its Authenticated flag) and before SetupGuard, so
	// a page that only redirects to `/setup` still counts as operator
	// activity.
	if o.heartbeatRecorder != nil {
		engine.Use(middleware.Heartbeat(o.heartbeatRecorder))
	}
	settingsStore := o.secretsStore
	if settingsStore == nil {
		settingsStore = handler.StaticSecretsStore{}
	} else {
		// Registered before any route so it also covers NoRoute and the
		// `/api/v1` group; StaticFS below is exempt inside the guard.
		engine.Use(middleware.SetupGuard(settingsStore, config.RequiredSecretKeys()))
	}
	// Header (every full page) SSRs the Kill Switch panel's state and
	// allowed actions from this; it reads lazily, so /static, fragments,
	// APIs and WebSockets pay nothing.
	engine.Use(middleware.SystemState(o.systemEngine))
	engine.StaticFS("/static", staticFS())
	// Scanner Dashboard is the app's home page (organisms/header.templ's nav
	// lists it first); `/` used to serve a static "Backend skeleton is
	// running." placeholder left over from #2's initial scaffold, which is
	// what a freshly launched cmd/desktop window (Wails has no configured
	// start URL, so it loads "/") or `go run ./cmd/server` + browser would
	// show instead of any real screen.
	engine.GET("/", func(c *gin.Context) { c.Redirect(http.StatusFound, "/scanner") })
	if swaggerEnabled() {
		engine.GET("/swagger", handler.SwaggerUI)
	}

	scannerHandler := handler.NewScannerHandler(o.candidateSource, o.candidateRefresh)
	engine.GET("/scanner", scannerHandler.Page)
	engine.GET("/ws/scanner", scannerHandler.WebSocket)

	systemHandler := handler.NewSystemHandler(o.systemEngine)
	engine.POST("/system/pause", systemHandler.Pause)
	engine.POST("/system/resume", systemHandler.Resume)
	engine.POST("/system/kill", systemHandler.Kill)
	engine.GET("/system/status", systemHandler.Status)
	engine.GET("/ws/system", systemHandler.WebSocket)

	symbolHandler := handler.NewSymbolHandler(o.symbolProvider, o.symbolRiskParams)
	engine.GET("/symbols/:symbol", symbolHandler.Page)
	engine.POST("/positions/:id/close", symbolHandler.ClosePosition)
	engine.GET("/ws/symbols/:symbol", symbolHandler.WebSocket)

	calibrationHandler := handler.NewCalibrationHandler(o.calibrationSource)
	engine.GET("/calibration", calibrationHandler.Page)

	performanceHandler := handler.NewPerformanceHandler(o.backtestRunner)
	engine.GET("/performance", performanceHandler.Page)

	activityHandler := handler.NewActivityHandler(o.activitySource)
	engine.GET("/activity", activityHandler.Page)
	engine.GET("/ws/activity", activityHandler.WebSocket)

	settingsHandler := handler.NewSettingsHandler(settingsStore)
	engine.GET("/setup", settingsHandler.SetupPage)
	engine.GET("/settings", settingsHandler.Page)
	engine.POST("/settings/:key", settingsHandler.Save)
	engine.DELETE("/settings/:key", settingsHandler.Delete)
	engine.GET("/system/secrets-status", settingsHandler.Status)

	updateHandler := handler.NewUpdateHandler(o.updateController)
	engine.GET("/system/update-status", updateHandler.Status)
	engine.GET("/system/update-panel", updateHandler.Panel)
	engine.POST("/system/update-check", updateHandler.Check)

	apiConfig := huma.DefaultConfig("pitha-trador API", "0.1.0")
	// The Stoplight Elements UI is already served at `/swagger` pointed at
	// this same openapi.json (handler.SwaggerUI); disable Huma's built-in
	// docs route so there isn't a second, unlinked copy.
	apiConfig.DocsPath = ""
	api := humagin.NewWithGroup(engine, engine.Group("/api/v1"), apiConfig)
	huma.Get(api, "/scanner", scannerHandler.APIScanner)
	huma.Post(api, "/system/pause", systemHandler.APIPause)
	huma.Post(api, "/system/resume", systemHandler.APIResume)
	huma.Post(api, "/system/kill", systemHandler.APIKill)
	huma.Get(api, "/system/status", systemHandler.APIStatus)
	huma.Get(api, "/symbols/{symbol}", symbolHandler.APISymbol)
	huma.Get(api, "/symbols/{symbol}/candles", symbolHandler.APICandles)
	huma.Get(api, "/positions", symbolHandler.APIPositions)
	huma.Get(api, "/orders", symbolHandler.APIOrders)
	insightapi.New(o.insightProvider).Register(api)
	huma.Get(api, "/calibration", calibrationHandler.APICalibration)
	huma.Get(api, "/policy-proposals", handler.NewPolicyProposalHandler(o.proposalSource).APIPolicyProposals)
	huma.Get(api, "/activity", activityHandler.APIActivity)

	return engine
}

// swaggerEnabled reports whether the `/swagger` route (Stoplight Elements
// UI, docs/environment/setup.md "Swagger / OpenAPI") should be registered.
// Controlled by the SWAGGER_ENABLED env var: opt-in, so the docs UI is
// disabled unless explicitly set to "true" (`make dev` does; production
// and Phase 7 live trading never expose it, issue #112).
func swaggerEnabled() bool {
	return os.Getenv("SWAGGER_ENABLED") == "true"
}
