// Command desktop is the Wails v2 entrypoint for the pitha-trador native
// desktop shell. It builds the shared Gin engine (internal/router) and
// injects it as the Wails AssetServer.Handler, so the WebView renders
// whatever Gin/HTMX/Templ returns instead of Wails' default embedded
// frontend/ assets (docs/components/overview.md §7).
package main

import (
	"log"
	"log/slog"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap"
	"github.com/ousiassllc/pitha-trador/internal/logging"
	"github.com/ousiassllc/pitha-trador/internal/router"
)

// logDir is where RotatingWriter writes today's structured JSON log
// file (requirements/non-functional.md §5). A later composition-root
// step may make this configurable; every entrypoint (cmd/desktop,
// cmd/server) uses the same relative "logs" directory today.
const logDir = "logs"

func main() {
	logWriter, err := logging.NewRotatingWriter(logDir)
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

	app := NewApp()
	engine := router.New()

	if err := wails.Run(&options.App{
		Title:  "pitha-trador",
		Width:  1280,
		Height: 800,
		AssetServer: &assetserver.Options{
			Handler: engine,
		},
		OnStartup: app.startup,
		Bind: []interface{}{
			app,
		},
	}); err != nil {
		log.Fatal(err)
	}
}
