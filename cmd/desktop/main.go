// Command desktop is the Wails v2 entrypoint for the pitha-trador native
// desktop shell. It builds the shared Gin engine (internal/router) and
// injects it as the Wails AssetServer.Handler, so the WebView renders
// whatever Gin/HTMX/Templ returns instead of Wails' default embedded
// frontend/ assets (docs/components/overview.md §7).
package main

import (
	"context"
	"errors"
	"log"
	"log/slog"
	"os"
	"os/signal"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap"
	"github.com/ousiassllc/pitha-trador/internal/logging"
	"github.com/ousiassllc/pitha-trador/internal/router"
	"github.com/ousiassllc/pitha-trador/internal/singleinstance"
	"github.com/ousiassllc/pitha-trador/internal/supervisor"
	"github.com/ousiassllc/pitha-trador/internal/web/middleware"
)

func main() {
	// The installer's Startup shortcut launches `pitha-trador.exe
	// --supervise` (docs/requirements/non-functional.md §3): that process
	// only supervises, restarting the real app after a crash.
	if childArgs, ok := supervisor.ChildArgs(os.Args[1:]); ok {
		superviseSelf(childArgs)
		return
	}

	logWriter, err := logging.NewRotatingWriter(bootstrap.LogDir)
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = logWriter.Close() }()
	slog.SetDefault(logging.New(logWriter, slog.LevelInfo))

	// Single-instance guard, taken before bootstrap.Run/BuildServices so a
	// second launch (desktop icon while the --supervise autostart instance
	// runs) never recovers the first instance's running jobs or starts a
	// second Scheduler/Kill Switch/order flow against the shared DB. Exit
	// code 0 also ends a supervisor that spawned this process.
	lock, err := bootstrap.AcquireInstanceLock(bootstrap.AppLockName)
	if errors.Is(err, singleinstance.ErrAlreadyRunning) {
		slog.Info("desktop: another instance is already running; exiting")
		return
	}
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = lock.Release() }()

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

	secretsRepo, secrets, err := bootstrap.LoadSecrets(context.Background(), state)
	if err != nil {
		log.Fatal(err)
	}

	// app is also the Risk Engine's native OS toast Notifier (notify.go),
	// fanned out alongside the structured-log and Slack channels
	// BuildServices always wires (issue #48), and cmd/desktop's
	// internal/service/updater.Quitter (app.go's QuitForUpdate) - issue
	// #65's unattended self-update, which cmd/server never wires in at
	// all.
	app := NewApp()
	services := bootstrap.BuildServices(state, secrets, bootstrap.WithAutoUpdate(app), bootstrap.WithNotifiers(app))
	app.services = services

	wsListeners, wsBase := listenWebSocket()

	engine := router.New(append(
		bootstrap.RouterOptions(services, state, secretsRepo),
		router.WithWebSocketBase(wsBase),
		router.WithAllowedHosts(middleware.WailsHosts()...),
		router.WithUpdateController(services.Updater),
	)...)

	if len(wsListeners) > 0 {
		defer serveWebSocket(wsListeners, engine)()
	}

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

// superviseSelf re-executes this binary (without --supervise) and restarts
// it after every abnormal exit. It returns once the child exits cleanly
// (operator quit / self-update) or the supervisor itself is interrupted.
func superviseSelf(childArgs []string) {
	// The child appends to the same daily JSON log file (O_APPEND), so
	// crash/restart records land next to the child's own output.
	logWriter, err := logging.NewRotatingWriter(bootstrap.LogDir)
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = logWriter.Close() }()
	slog.SetDefault(logging.New(logWriter, slog.LevelInfo))

	// A second watcher would spawn a second app instance on every crash.
	lock, err := bootstrap.AcquireInstanceLock(bootstrap.SupervisorLockName)
	if errors.Is(err, singleinstance.ErrAlreadyRunning) {
		slog.Info("supervisor: another supervisor is already running; exiting")
		return
	}
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = lock.Release() }()

	exe, err := os.Executable()
	if err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	_ = supervisor.Run(ctx, supervisor.Config{}, supervisor.ExecRun(exe, childArgs...))
}
