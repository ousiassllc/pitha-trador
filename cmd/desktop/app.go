package main

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap"
	"github.com/ousiassllc/pitha-trador/internal/service/updater"
	"github.com/ousiassllc/pitha-trador/internal/service/updater/tempcleanup"
)

// App is the Wails-bound application struct. It holds the Wails runtime
// context (required by every runtime.* call, e.g. notify.go's native
// notifications) and owns the background services' lifecycle: startup
// launches them, shutdown stops them (docs/architecture/overview.md §9).
type App struct {
	ctx context.Context

	// services is set by main once bootstrap.BuildServices has returned
	// (App itself is one of BuildServices' Notifiers, so it must exist
	// first).
	services *bootstrap.Services
	cancel   context.CancelFunc

	// mu guards pendingInstaller: QuitForUpdate (issue #65) runs on
	// internal/service/scheduler's own cron goroutine, while shutdown
	// runs on Wails' separate shutdown goroutine.
	mu               sync.Mutex
	pendingInstaller string

	// initNotifications/cleanupNotifications are Wails' native notification
	// lifecycle calls; they are fields only so tests can observe them
	// without a Wails runtime.
	initNotifications    func(context.Context) error
	cleanupNotifications func(context.Context)
}

// NewApp creates a new App instance.
func NewApp() *App {
	return &App{
		initNotifications:    runtime.InitializeNotifications,
		cleanupNotifications: runtime.CleanupNotifications,
	}
}

// setupNotifications initializes Wails' native notification service, which
// runtime.SendNotification requires before its first use ("This must be
// called before sending any notifications"; on Windows it sets the toast
// AppID, icon and activation callback - issue #549). A failure is logged and
// otherwise ignored: the app still runs, and KillSwitchTriggered /
// KillSwitchAutoResumed then return (and the risk fan-out logs) the
// SendNotification error.
func (a *App) setupNotifications(ctx context.Context) {
	if err := a.initNotifications(ctx); err != nil {
		slog.Error("desktop: initialize native notifications failed", "error", err)
	}
}

// startup is Wails' OnStartup hook: it saves the runtime context, then
// starts every background goroutine the composition root owns (Scheduler
// workers/cron, kabuステーションAPI token refresh, candidate refresh -
// bootstrap.Services.Start). A Start failure leaves the app unable to
// trade or scan, so it is reported in a native error dialog and the app
// quits rather than running with its background processing silently dead.
func (a *App) startup(ctx context.Context) {
	startedAt := time.Now()
	a.ctx = ctx
	a.setupNotifications(ctx)
	cleanupStaleUpdateDownloads(startedAt)

	runCtx, cancel := context.WithCancel(ctx)
	a.cancel = cancel
	if err := a.services.Start(runCtx); err != nil {
		cancel()
		slog.Error("desktop: start background services failed", "error", err)
		_, _ = runtime.MessageDialog(ctx, runtime.MessageDialogOptions{
			Type:    runtime.ErrorDialog,
			Title:   "pitha-trador の起動に失敗しました",
			Message: err.Error(),
		})
		runtime.Quit(ctx)
	}
}

// cleanupStaleUpdateDownloads removes the installer directories a previous
// self-update left in the temp directory (tempcleanup.CleanupStale;
// the installer cannot delete itself, so the restarted app does). A failure
// is only logged: it must never keep the app from starting.
func cleanupStaleUpdateDownloads(startedAt time.Time) {
	removed, err := tempcleanup.CleanupStale(startedAt)
	if err != nil {
		slog.Warn("desktop: clean up stale update downloads failed", "removed", removed, "error", err)
		return
	}
	if removed > 0 {
		slog.Info("desktop: removed stale update downloads", "removed", removed)
	}
}

// QuitForUpdate implements internal/service/updater.Quitter (issue #65):
// internal/bootstrap's updater.SchedulerAdapter calls this once
// updater.Checker.CheckForUpdate has downloaded and verified a newer
// installer. It records installerPath for shutdown (below) to launch,
// then triggers Wails' normal graceful-quit path (runtime.Quit) - the
// same path startup's own error case above already uses - so shutdown
// blocks on Services.Stop() before the installer ever runs, and no
// in-flight job or DB write races the restart.
func (a *App) QuitForUpdate(installerPath string) {
	a.mu.Lock()
	a.pendingInstaller = installerPath
	a.mu.Unlock()
	runtime.Quit(a.ctx)
}

// shutdown is Wails' OnShutdown hook: it cancels the context startup
// passed to Services.Start and blocks until every background goroutine
// (and in-flight Scheduler job) has exited, so main's deferred DB close
// never races a running job. Once every goroutine has stopped, if
// QuitForUpdate recorded a verified installer (issue #65), it launches it
// fully unattended (`installer.exe /S`) as a detached process; the NSIS
// installer itself (build/windows/installer/project.nsi's silent-launch
// customization) restarts the new pitha-trador.exe once installation
// completes.
func (a *App) shutdown(ctx context.Context) {
	if a.cancel != nil {
		a.cancel()
	}
	a.services.Stop()
	a.cleanupNotifications(ctx)

	a.mu.Lock()
	installerPath := a.pendingInstaller
	a.mu.Unlock()
	if installerPath == "" {
		return
	}
	if err := updater.BuildSilentInstallCommand(installerPath).Start(); err != nil {
		slog.Error("desktop: launch self-update installer failed", "installer", installerPath, "error", err)
		return
	}
	slog.Info("desktop: self-update installer launched", "installer", installerPath)
}
