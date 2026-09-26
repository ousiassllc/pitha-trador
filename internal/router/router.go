// Package router builds the single Gin engine shared by cmd/desktop (via
// Wails' options.App.AssetServer.Handler) and cmd/server (via net/http). It
// intentionally has no dependency on Wails so that a headless server can be
// run without a Wails process (see docs/architecture/overview.md §9).
package router

import (
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humagin"
	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/web/handler"
)

const placeholderHTML = `<!DOCTYPE html>
<html lang="ja">
<head>
  <meta charset="utf-8">
  <title>pitha-trador</title>
</head>
<body>
  <h1>pitha-trador</h1>
  <p>Backend skeleton is running.</p>
</body>
</html>
`

// staticDir is static/src's absolute path, resolved relative to this
// source file rather than the process's current working directory so
// `/static/...` serves the same esbuild/Tailwind output
// (components/overview.md §2) regardless of whether the caller is `go
// test`, `cmd/server`, or `wails dev` (each has a different cwd). Once
// `wails build` packages a single .exe (architecture/overview.md §7), this
// is replaced by an embed.FS baked in at build time instead of reading
// from disk.
var staticDir = func() string {
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "static", "src")
}()

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
	candidateSource  handler.CandidateSource
	candidateRefresh handler.CandidateRefreshInterval
	systemEngine     handler.SystemEngine
	symbolProvider   handler.SymbolProvider
	symbolRiskParams handler.SymbolRiskParams
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
// internal/web/handler.SystemEngine. Defaults to a Running
// handler.StaticSystemEngine until a later sub-scope wires a real
// internal/service/risk.Engine in.
func WithSystemEngine(engine handler.SystemEngine) Option {
	return func(o *options) { o.systemEngine = engine }
}

// WithSymbolProvider overrides the Symbol Detail/position/order routes'
// backing internal/web/handler.SymbolProvider. Defaults to an empty
// handler.StaticSymbolProvider until a later sub-scope wires a real
// internal/service/execution.Engine in.
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

// New builds and returns the shared Gin engine: the placeholder root
// page, the `/swagger` API docs UI, the Huma-based `/api/v1` JSON API, and
// the Scanner Dashboard SSR/WebSocket routes (docs/api/endpoints.md).
// Route registration otherwise stays minimal at this stage; later
// sub-scopes register the remaining SSR routes (Templ/HTMX, via
// internal/web/handler) on top of this engine.
func New(opts ...Option) *gin.Engine {
	o := options{
		candidateSource:  handler.StaticCandidateSource{},
		candidateRefresh: defaultCandidateRefreshInterval,
		systemEngine:     handler.StaticSystemEngine{},
		symbolProvider:   handler.StaticSymbolProvider{},
		symbolRiskParams: defaultSymbolRiskParams,
	}
	for _, opt := range opts {
		opt(&o)
	}

	engine := gin.New()
	engine.Static("/static", staticDir)
	engine.GET("/", handlePlaceholder)
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

	return engine
}

// swaggerEnabled reports whether the `/swagger` route (Stoplight Elements
// UI, docs/environment/setup.md "Swagger / OpenAPI") should be registered.
// Controlled by the SWAGGER_ENABLED env var: defaults to true (dev/staging)
// and is disabled only when explicitly set to "false" (production, Phase 7
// live trading).
func swaggerEnabled() bool {
	return os.Getenv("SWAGGER_ENABLED") != "false"
}

func handlePlaceholder(c *gin.Context) {
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(placeholderHTML))
}
