// Command desktop is the Wails v2 entrypoint for the pitha-trador native
// desktop shell. It builds the shared Gin engine (internal/router) and
// injects it as the Wails AssetServer.Handler, so the WebView renders
// whatever Gin/HTMX/Templ returns instead of Wails' default embedded
// frontend/ assets (docs/components/overview.md §7).
package main

import (
	"log"
	"log/slog"
	"time"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap"
	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/logging"
	"github.com/ousiassllc/pitha-trador/internal/router"
	"github.com/ousiassllc/pitha-trador/internal/web/handler"
)

func main() {
	logWriter, err := logging.NewRotatingWriter(bootstrap.LogDir)
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = logWriter.Close() }()
	slog.SetDefault(logging.New(logWriter, slog.LevelInfo))

	// bootstrap.Run opens (creating/migrating) the SQLite DB and loads
	// config/strategy.yaml + config/risk.yaml (issue #42,
	// docs/architecture/overview.md §10.1). A failure here (unopenable
	// DB, unparsable config) is fatal: this build shows no native error
	// dialog for it, since doing so before the first WebView window
	// exists would need standalone platform-native dialog handling
	// (Wails' own runtime.MessageDialog requires the ctx OnStartup
	// provides, which does not exist yet at this point) - added
	// complexity for a rare failure path when log.Fatal (the same
	// decision cmd/server/main.go makes, and what the RotatingWriter
	// failure above already does) already surfaces the error in the
	// structured JSON log file operators check first.
	state, err := bootstrap.Run(bootstrap.Config{})
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = state.Close() }()

	// config.LoadSecrets reads JEV_API_KEY/JEV_BASE_URL/KABU_API_PASSWORD
	// (issue #43); a missing value fails startup outright rather than
	// silently falling back to a mock client (issue #43's own
	// error-handling decision - internal/bootstrap.BuildServices' real
	// kabuステーションAPI client wiring, issue #44, depends on it).
	secrets, err := config.LoadSecrets()
	if err != nil {
		log.Fatal(err)
	}

	// app is also the Risk Engine's native OS toast Notifier (notify.go),
	// fanned out alongside the structured-log and Slack channels
	// BuildServices always wires (issue #48).
	app := NewApp()
	services, err := bootstrap.BuildServices(state, secrets, app)
	if err != nil {
		log.Fatal(err)
	}
	app.services = services

	engine := router.New(
		router.WithCandidateSource(services.Screener),
		router.WithSystemEngine(services.Risk),
		router.WithSymbolProvider(services.Execution),
		router.WithCalibrationSource(services.Calibration),
		router.WithBacktestRunner(services.Backtest),
		router.WithCandidateRefreshInterval(handler.CandidateRefreshInterval{
			Min: time.Duration(state.Strategy.Scan.CandidateRefreshIntervalSecondsMin) * time.Second,
			Max: time.Duration(state.Strategy.Scan.CandidateRefreshIntervalSecondsMax) * time.Second,
		}),
	)

	if err := wails.Run(&options.App{
		Title:  "pitha-trador",
		Width:  1280,
		Height: 800,
		AssetServer: &assetserver.Options{
			Handler: engine,
		},
		OnStartup:  app.startup,
		OnShutdown: app.shutdown,
		Bind: []interface{}{
			app,
		},
	}); err != nil {
		log.Fatal(err)
	}
}
