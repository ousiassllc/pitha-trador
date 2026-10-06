package router

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humagin"
	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/web/apierror"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/activity"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/calibration"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/performance"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/proposals"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/scanner"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/settings"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/swagger"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/symbol"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/system"
	"github.com/ousiassllc/pitha-trador/internal/web/insightapi"
)

// handlers holds the handlers shared between the SSR routes
// (registerPages) and the `/api/v1` JSON API (registerAPI).
type handlers struct {
	scanner     *scanner.ScannerHandler
	system      *system.SystemHandler
	symbol      *symbol.SymbolHandler
	calibration *calibration.CalibrationHandler
	activity    *activity.ActivityHandler
}

// registerPages registers the static assets and every SSR/WebSocket route
// on engine and returns the handlers registerAPI reuses.
func registerPages(engine *gin.Engine, o options, settingsStore settings.SecretsStore) handlers {
	engine.StaticFS("/static", staticFS())
	// Scanner Dashboard is the app's home page (organisms/header.templ's nav
	// lists it first); `/` used to serve a static "Backend skeleton is
	// running." placeholder left over from #2's initial scaffold, which is
	// what a freshly launched cmd/desktop window (Wails has no configured
	// start URL, so it loads "/") or `go run ./cmd/server` + browser would
	// show instead of any real screen.
	engine.GET("/", func(c *gin.Context) { c.Redirect(http.StatusFound, "/scanner") })
	if swaggerEnabled() {
		engine.GET("/swagger", swagger.SwaggerUI)
	}
	h := handlers{
		scanner:     scanner.NewScannerHandler(o.candidateSource, o.candidateRefresh),
		system:      system.NewSystemHandler(o.systemEngine),
		symbol:      symbol.NewSymbolHandler(o.symbolProvider, o.symbolRiskParams),
		calibration: calibration.NewCalibrationHandler(o.calibrationSource),
		activity:    activity.NewActivityHandler(o.activitySource),
	}
	h.scanner.SetUniverseImporter(o.universeImporter)
	engine.GET("/scanner", h.scanner.Page)
	engine.GET("/scanner/scan", h.scanner.ScanView)
	engine.POST("/scanner/universe/import", h.scanner.UniverseImport)
	engine.GET("/ws/scanner", h.scanner.WebSocket)

	engine.GET("/system/status", h.system.Status)
	engine.GET("/ws/system", h.system.WebSocket)

	engine.GET("/symbols/:symbol", h.symbol.Page)
	engine.POST("/positions/:id/close", h.symbol.ClosePosition)
	engine.GET("/ws/symbols/:symbol", h.symbol.WebSocket)

	engine.GET("/calibration", h.calibration.Page)

	performanceHandler := performance.NewPerformanceHandler(o.backtestRunner, o.insightProvider)
	engine.GET("/performance", performanceHandler.Page)

	engine.GET("/activity", h.activity.Page)
	engine.GET("/ws/activity", h.activity.WebSocket)

	settingsHandler := settings.NewSettingsHandler(settingsStore)
	engine.GET("/setup", settingsHandler.SetupPage)
	engine.GET("/settings", settingsHandler.Page)
	engine.POST("/settings/:key", settingsHandler.Save)
	engine.DELETE("/settings/:key", settingsHandler.Delete)
	engine.GET("/system/secrets-status", settingsHandler.Status)

	updateHandler := system.NewUpdateHandler(o.updateController)
	engine.GET("/system/update-status", updateHandler.Status)
	engine.GET("/system/update-panel", updateHandler.Panel)
	engine.POST("/system/update-check", updateHandler.Check)

	engine.GET("/system/marketdata-status", system.NewMarketDataHandler(o.marketDataStatus).Status)

	return h
}

// apiBasePath is the mount point of the Huma JSON API.
const apiBasePath = "/api/v1"

// registerAPI registers the Huma-based `/api/v1` JSON API on engine.
func registerAPI(engine *gin.Engine, o options, h handlers) {
	// 5xx bodies carry only the fixed message; causes go to slog (issue #215).
	apierror.Install()
	apiConfig := huma.DefaultConfig("pitha-trador API", "0.1.0")
	// The Stoplight Elements UI is already served at `/swagger` pointed at
	// this same openapi.json (swagger.SwaggerUI); disable Huma's built-in
	// docs route so there isn't a second, unlinked copy.
	apiConfig.DocsPath = ""
	// The group mounts the API under /api/v1; Servers tells Huma (and OpenAPI
	// clients such as Elements "Try It") about that base URL so the spec,
	// `$schema` and `Link: rel="describedBy"` include the prefix (issue #234).
	apiConfig.Servers = []*huma.Server{{URL: apiBasePath}}
	api := humagin.NewWithGroup(engine, engine.Group(apiBasePath), apiConfig)
	huma.Get(api, "/scanner", h.scanner.APIScanner)
	huma.Get(api, "/scanner/scan", h.scanner.APIScannerScan)
	huma.Get(api, "/scanner/scan/export", h.scanner.APIScannerScanExport)
	huma.Post(api, "/system/pause", h.system.APIPause)
	huma.Post(api, "/system/resume", h.system.APIResume)
	huma.Post(api, "/system/kill", h.system.APIKill)
	huma.Get(api, "/system/status", h.system.APIStatus)
	huma.Get(api, "/symbols/{symbol}", h.symbol.APISymbol)
	huma.Get(api, "/symbols/{symbol}/candles", h.symbol.APICandles)
	huma.Get(api, "/positions", h.symbol.APIPositions)
	huma.Get(api, "/orders", h.symbol.APIOrders)
	huma.Get(api, "/logs/errors", system.NewErrorLogHandler(o.errorLogExporter).APIErrorLogs)
	insightapi.New(o.insightProvider).Register(api)
	huma.Get(api, "/calibration", h.calibration.APICalibration)
	huma.Get(api, "/policy-proposals", proposals.NewPolicyProposalHandler(o.proposalSource).APIPolicyProposals)
	huma.Get(api, "/activity", h.activity.APIActivity)
}
