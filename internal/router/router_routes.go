package router

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humagin"
	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/web/apierror"
	"github.com/ousiassllc/pitha-trador/internal/web/handler"
	"github.com/ousiassllc/pitha-trador/internal/web/insightapi"
)

// handlers holds the handlers shared between the SSR routes
// (registerPages) and the `/api/v1` JSON API (registerAPI).
type handlers struct {
	scanner     *handler.ScannerHandler
	system      *handler.SystemHandler
	symbol      *handler.SymbolHandler
	calibration *handler.CalibrationHandler
	activity    *handler.ActivityHandler
}

// registerPages registers the static assets and every SSR/WebSocket route
// on engine and returns the handlers registerAPI reuses.
func registerPages(engine *gin.Engine, o options, settingsStore handler.SecretsStore) handlers {
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
	h := handlers{
		scanner:     handler.NewScannerHandler(o.candidateSource, o.candidateRefresh),
		system:      handler.NewSystemHandler(o.systemEngine),
		symbol:      handler.NewSymbolHandler(o.symbolProvider, o.symbolRiskParams),
		calibration: handler.NewCalibrationHandler(o.calibrationSource),
		activity:    handler.NewActivityHandler(o.activitySource),
	}
	engine.GET("/scanner", h.scanner.Page)
	engine.GET("/ws/scanner", h.scanner.WebSocket)

	engine.GET("/system/status", h.system.Status)
	engine.GET("/ws/system", h.system.WebSocket)

	engine.GET("/symbols/:symbol", h.symbol.Page)
	engine.POST("/positions/:id/close", h.symbol.ClosePosition)
	engine.GET("/ws/symbols/:symbol", h.symbol.WebSocket)

	engine.GET("/calibration", h.calibration.Page)

	performanceHandler := handler.NewPerformanceHandler(o.backtestRunner)
	engine.GET("/performance", performanceHandler.Page)

	engine.GET("/activity", h.activity.Page)
	engine.GET("/ws/activity", h.activity.WebSocket)

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

	return h
}

// registerAPI registers the Huma-based `/api/v1` JSON API on engine.
func registerAPI(engine *gin.Engine, o options, h handlers) {
	// 5xx bodies carry only the fixed message; causes go to slog (issue #215).
	apierror.Install()
	apiConfig := huma.DefaultConfig("pitha-trador API", "0.1.0")
	// The Stoplight Elements UI is already served at `/swagger` pointed at
	// this same openapi.json (handler.SwaggerUI); disable Huma's built-in
	// docs route so there isn't a second, unlinked copy.
	apiConfig.DocsPath = ""
	api := humagin.NewWithGroup(engine, engine.Group("/api/v1"), apiConfig)
	huma.Get(api, "/scanner", h.scanner.APIScanner)
	huma.Post(api, "/system/pause", h.system.APIPause)
	huma.Post(api, "/system/resume", h.system.APIResume)
	huma.Post(api, "/system/kill", h.system.APIKill)
	huma.Get(api, "/system/status", h.system.APIStatus)
	huma.Get(api, "/symbols/{symbol}", h.symbol.APISymbol)
	huma.Get(api, "/symbols/{symbol}/candles", h.symbol.APICandles)
	huma.Get(api, "/positions", h.symbol.APIPositions)
	huma.Get(api, "/orders", h.symbol.APIOrders)
	insightapi.New(o.insightProvider).Register(api)
	huma.Get(api, "/calibration", h.calibration.APICalibration)
	huma.Get(api, "/policy-proposals", handler.NewPolicyProposalHandler(o.proposalSource).APIPolicyProposals)
	huma.Get(api, "/activity", h.activity.APIActivity)
}
