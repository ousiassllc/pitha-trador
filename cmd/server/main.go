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
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap"
	"github.com/ousiassllc/pitha-trador/internal/logging"
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

// shutdownTimeout bounds how long a SIGINT/SIGTERM waits for in-flight
// HTTP requests (including open WebSocket streams) before closing them.
const shutdownTimeout = 10 * time.Second

func main() {
	logWriter, err := logging.NewRotatingWriter(bootstrap.LogDir)
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = logWriter.Close() }()
	slog.SetDefault(logging.New(logWriter, slog.LevelInfo))

	// bootstrap.Run opens (creating/migrating) the SQLite DB and loads
	// config/strategy.yaml + config/risk.yaml (issue #42,
	// docs/architecture/overview.md §10.1). This entrypoint has no
	// window at all, so log.Fatal on failure (same as the RotatingWriter
	// failure above) is the only sensible option.
	state, err := bootstrap.Run(bootstrap.Config{})
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = state.Close() }()

	secretsRepo, secrets, err := bootstrap.LoadSecrets(context.Background(), state)
	if err != nil {
		log.Fatal(err)
	}

	// No WithAutoUpdate: cmd/server is headless and has no installer to run, so
	// issue #65's unattended self-update never wires in here.
	services, err := bootstrap.BuildServices(state, secrets)
	if err != nil {
		log.Fatal(err)
	}

	allowNonLoopback := os.Getenv(EnvAllowNonLoopback) == "1"
	addr, err := resolveListenAddr(os.Getenv("PITHA_SERVER_ADDR"), allowNonLoopback)
	if err != nil {
		log.Fatal(err)
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
		log.Fatal(err)
	}

	srv := &http.Server{
		Addr:              addr,
		Handler:           engine,
		ReadHeaderTimeout: 10 * time.Second,
	}
	serveErr := make(chan error, 1)
	go func() {
		log.Printf("pitha-trador server listening on %s", addr) //nolint:gosec // G706: addr is the server's own fixed listen address, not user input
		err := safego.Try("http server", srv.ListenAndServe)
		serveErr <- err
	}()

	select {
	case err := <-serveErr:
		stop()
		services.Stop()
		log.Fatalf("server error: %v", err)
	case <-ctx.Done():
	}

	log.Print("pitha-trador server shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("server: graceful HTTP shutdown failed", "error", err)
	}
	// Blocks until every Scheduler worker (and its in-flight job) has
	// exited, before the deferred state.Close closes the DB under them.
	services.Stop()
}
