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
	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/logging"
	"github.com/ousiassllc/pitha-trador/internal/router"
	"github.com/ousiassllc/pitha-trador/internal/web/handler"
)

// defaultAddr uses 48080 instead of the far more commonly-claimed 8080
// (Tomcat/many Node dev servers/etc.) or kabuステーションAPIの18080
// (internal/service/marketdata, docs/architecture/overview.md §5) to
// minimize the odds of a port clash with other local services.
const defaultAddr = ":48080"

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

	// config.LoadSecrets reads JEV_API_KEY/JEV_BASE_URL/KABU_API_PASSWORD
	// (issue #43); a missing value fails startup outright rather than
	// silently falling back to a mock client (issue #43's own
	// error-handling decision - internal/bootstrap.BuildServices' real
	// kabuステーションAPI client wiring, issue #44, depends on it).
	secrets, err := config.LoadSecrets()
	if err != nil {
		log.Fatal(err)
	}

	services, err := bootstrap.BuildServices(state, secrets)
	if err != nil {
		log.Fatal(err)
	}

	addr := os.Getenv("PITHA_SERVER_ADDR")
	if addr == "" {
		addr = defaultAddr
	}

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
		log.Printf("pitha-trador server listening on %s", addr)
		serveErr <- srv.ListenAndServe()
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
