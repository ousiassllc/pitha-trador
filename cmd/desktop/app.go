package main

import (
	"context"
	"log/slog"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap"
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
}

// NewApp creates a new App instance.
func NewApp() *App {
	return &App{}
}

// startup is Wails' OnStartup hook: it saves the runtime context, then
// starts every background goroutine the composition root owns (Scheduler
// workers/cron, kabuステーションAPI token refresh, candidate refresh -
// bootstrap.Services.Start). A Start failure leaves the app unable to
// trade or scan, so it is reported in a native error dialog and the app
// quits rather than running with its background processing silently dead.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

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

// shutdown is Wails' OnShutdown hook: it cancels the context startup
// passed to Services.Start and blocks until every background goroutine
// (and in-flight Scheduler job) has exited, so main's deferred DB close
// never races a running job.
func (a *App) shutdown(context.Context) {
	if a.cancel != nil {
		a.cancel()
	}
	a.services.Stop()
}
