// Command server runs the pitha-trador backend headlessly (no Wails, no
// native window) by listening directly with net/http. It shares the exact
// same Gin engine as cmd/desktop via internal/router, which has no
// dependency on Wails (docs/architecture/overview.md §9).
//
// This entrypoint is intended for environments where the Wails/WebView2
// native shell cannot run (CI, headless test environments, ...).
package main

import (
	"log"
	"log/slog"
	"net/http"
	"os"
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

// logDir is where RotatingWriter writes today's structured JSON log
// file (requirements/non-functional.md §5); the same relative "logs"
// directory cmd/desktop's own entrypoint uses.
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
		router.WithCandidateRefreshInterval(handler.CandidateRefreshInterval{
			Min: time.Duration(state.Strategy.Scan.CandidateRefreshIntervalSecondsMin) * time.Second,
			Max: time.Duration(state.Strategy.Scan.CandidateRefreshIntervalSecondsMax) * time.Second,
		}),
	)

	srv := &http.Server{
		Addr:              addr,
		Handler:           engine,
		ReadHeaderTimeout: 10 * time.Second,
	}

	log.Printf("pitha-trador server listening on %s", addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
	}
}
