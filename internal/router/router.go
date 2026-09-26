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

// defaultCandidateRefreshInterval mirrors config/strategy.yaml's
// scan.candidate_refresh_interval_seconds_min/max defaults
// (functional.md §4.3, §5.1 "候補銘柄更新周期（15〜30秒）").
var defaultCandidateRefreshInterval = handler.CandidateRefreshInterval{
	Min: 15 * time.Second,
	Max: 30 * time.Second,
}

type options struct {
	candidateSource  handler.CandidateSource
	candidateRefresh handler.CandidateRefreshInterval
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
	}
	for _, opt := range opts {
		opt(&o)
	}

	engine := gin.New()
	engine.GET("/", handlePlaceholder)
	if swaggerEnabled() {
		engine.GET("/swagger", handler.SwaggerUI)
	}

	scannerHandler := handler.NewScannerHandler(o.candidateSource, o.candidateRefresh)
	engine.GET("/scanner", scannerHandler.Page)
	engine.GET("/ws/scanner", scannerHandler.WebSocket)

	apiConfig := huma.DefaultConfig("pitha-trador API", "0.1.0")
	// The Stoplight Elements UI is already served at `/swagger` pointed at
	// this same openapi.json (handler.SwaggerUI); disable Huma's built-in
	// docs route so there isn't a second, unlinked copy.
	apiConfig.DocsPath = ""
	api := humagin.NewWithGroup(engine, engine.Group("/api/v1"), apiConfig)
	huma.Get(api, "/scanner", scannerHandler.APIScanner)

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
