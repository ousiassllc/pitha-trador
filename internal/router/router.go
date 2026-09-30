// Package router builds the single Gin engine shared by cmd/desktop (via
// Wails' options.App.AssetServer.Handler) and cmd/server (via net/http). It
// intentionally has no dependency on Wails so that a headless server can be
// run without a Wails process (see docs/architecture/overview.md §9).
package router

import (
	"os"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/web/handler"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/activity"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/symbol"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/system"
	"github.com/ousiassllc/pitha-trador/internal/web/insightapi"
)

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
		systemEngine:      system.StaticSystemEngine{},
		symbolProvider:    symbol.StaticSymbolProvider{},
		insightProvider:   insightapi.StaticProvider{},
		calibrationSource: handler.StaticCalibrationSource{},
		proposalSource:    handler.StaticPolicyProposalSource{},
		backtestRunner:    handler.StaticBacktestRunner{},
		activitySource:    activity.StaticActivitySource{},
	}
	for _, opt := range opts {
		opt(&o)
	}

	engine := gin.New()
	settingsStore := useMiddleware(engine, o)
	h := registerPages(engine, o, settingsStore)
	registerAPI(engine, o, h)

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
