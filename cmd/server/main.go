// Command server runs the pitha-trador backend headlessly (no Wails, no
// native window) by listening directly with net/http. It shares the exact
// same Gin engine as cmd/desktop via internal/router, which has no
// dependency on Wails (docs/architecture/overview.md §9).
//
// This entrypoint is intended for environments where the Wails/WebView2
// native shell cannot run (CI, headless test environments, ...).
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap"
	"github.com/ousiassllc/pitha-trador/internal/bootstrap/startup"
	"github.com/ousiassllc/pitha-trador/internal/router"
	"github.com/ousiassllc/pitha-trador/internal/safego"
)

// defaultAddr uses 48080 instead of the far more commonly-claimed 8080
// (Tomcat/many Node dev servers/etc.) or kabuステーションAPIの18080
// (internal/service/marketdata, docs/architecture/overview.md §5) to
// minimize the odds of a port clash with other local services. The host is
// 127.0.0.1 so the server is unreachable from other machines
// (docs/api/endpoints.md §1); see EnvAllowNonLoopback in addr.go.
const defaultAddr = "127.0.0.1:48080"

// shutdownTimeout bounds each of the two SIGINT/SIGTERM shutdown phases
// (httpServer.shutdown): waiting for in-flight HTTP requests, then for the
// WebSocket handlers (hijacked connections http.Server.Shutdown neither
// waits for nor closes) to return once the server context is canceled.
const shutdownTimeout = 10 * time.Second

func main() {
	// run returns instead of exiting so its defers (DB Close, lock Release)
	// always run; RunMain then logs a returned error at ERROR level and
	// the process exits non-zero.
	os.Exit(startup.RunMain("server", bootstrap.ResolveLogDir, run))
}

func run() error {
	// Single-instance guard (the same app.lock cmd/desktop takes), acquired
	// before bootstrap.Run/BuildServices/Services.Start so a second process
	// never recovers the first one's running jobs or starts a second
	// Scheduler/PUSH subscription/Kill Switch against the shared DB.
	lock, err := bootstrap.AcquireInstanceLock(bootstrap.AppLockName)
	if err != nil {
		return err
	}
	defer func() { _ = lock.Release() }()

	// bootstrap.Run opens (creating/migrating) the SQLite DB and loads
	// config/strategy.yaml + config/risk.yaml (issue #42,
	// docs/architecture/overview.md §10.1). This entrypoint has no
	// window at all, so failing the process (RunMain's ERROR log and
	// non-zero exit) is the only sensible option.
	state, err := bootstrap.Run(bootstrap.Config{})
	if err != nil {
		return err
	}
	defer func() { _ = state.Close() }()

	secretsRepo, secrets, err := bootstrap.LoadSecrets(context.Background(), state)
	if err != nil {
		return err
	}

	// No WithAutoUpdate: cmd/server is headless and has no installer to run, so
	// issue #65's unattended self-update never wires in here.
	services := bootstrap.BuildServices(state, secrets)

	allowNonLoopback := os.Getenv(EnvAllowNonLoopback) == "1"
	addr, err := resolveListenAddr(os.Getenv("PITHA_SERVER_ADDR"), allowNonLoopback)
	if err != nil {
		return err
	}

	engine := router.New(append(
		bootstrap.RouterOptions(services, state, secretsRepo),
		router.WithAllowedHosts(allowedHosts(addr, allowNonLoopback, os.Getenv(EnvAllowedHosts))...),
	)...)

	// ctx is canceled on SIGINT/SIGTERM: the signal that stops the HTTP
	// server also stops every background goroutine Services.Start owns.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := services.Start(ctx); err != nil {
		return err
	}

	srv := newHTTPServer(addr, engine)
	serveErr := make(chan error, 1)
	go func() {
		slog.Info("pitha-trador server listening", "addr", addr)
		err := safego.Try("http server", srv.ListenAndServe)
		serveErr <- err
	}()

	select {
	case err := <-serveErr:
		stop()
		services.Stop()
		return fmt.Errorf("server error: %w", err)
	case <-ctx.Done():
	}

	slog.Info("pitha-trador server shutting down")
	if err := srv.shutdown(shutdownTimeout); err != nil {
		slog.Error("server: graceful HTTP shutdown failed", "error", err)
	}
	// Blocks until every Scheduler worker (and its in-flight job) has
	// exited, before the deferred state.Close closes the DB under them.
	// The WebSocket handlers have already returned (srv.shutdown).
	services.Stop()
	return nil
}
