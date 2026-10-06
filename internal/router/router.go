// Package router builds the single Gin engine shared by cmd/desktop (via
// Wails' options.App.AssetServer.Handler) and cmd/server (via net/http). It
// intentionally has no dependency on Wails so that a headless server can be
// run without a Wails process (see docs/architecture/overview.md §9).
package router

import (
	"os"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/web/handler/activity"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/calibration"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/performance"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/proposals"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/scanner"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/symbol"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/system"
	"github.com/ousiassllc/pitha-trador/internal/web/insightapi"
)

// New builds and returns the shared Gin engine in three stages:
// useMiddleware installs the cross-cutting middleware chain (in
// router_middleware.go), registerPages registers every SSR page, action
// and WebSocket route (`GET /` redirects to `/scanner`) plus the opt-in
// `/swagger` API docs UI, and registerAPI registers the Huma-based
// `/api/v1` JSON API. docs/api/endpoints.md is the source of truth for
// the route list.
func New(opts ...Option) *gin.Engine {
	o := options{
		candidateSource:   scanner.StaticCandidateSource{},
		candidateRefresh:  defaultCandidateRefreshInterval,
		systemEngine:      system.StaticSystemEngine{},
		symbolProvider:    symbol.StaticSymbolProvider{},
		insightProvider:   insightapi.StaticProvider{},
		calibrationSource: calibration.StaticCalibrationSource{},
		proposalSource:    proposals.StaticPolicyProposalSource{},
		backtestRunner:    performance.StaticBacktestRunner{},
		activitySource:    activity.StaticActivitySource{},
		errorLogExporter:  system.UnconfiguredErrorLogExporter{},
	}
	for _, opt := range opts {
		opt(&o)
	}

	engine := gin.New()
	settingsStore := useMiddleware(engine, o)
	engine.NoRoute(noRoute)
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
