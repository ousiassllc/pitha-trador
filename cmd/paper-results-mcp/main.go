// Command paper-results-mcp serves the read-only Paper results MCP over HTTP
// (issue #783; docs/architecture/paper-results-mcp.md).
//
// It is a separate process from the application (cmd/desktop, cmd/server)
// and is off by default: it binds nothing unless the local settings file
// (os.UserConfigDir()/pitha-trador/paper-results-mcp.json, i.e.
// %AppData%\pitha-trador\paper-results-mcp.json on Windows) sets
// "enabled": true together with a non-empty "api_key".
//
// The settings file path can be overridden for a run with -config; the
// database file is resolved like the application (PITHA_DB_PATH, else the
// per-user pitha.db) and is only ever opened read-only.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/settings"
)

func main() {
	if err := run(); err != nil {
		slog.Error("paper-results-mcp: exiting", "error", err)
		os.Exit(1)
	}
}

func run() error {
	configPath := flag.String("config", "", "settings file (default: <user config dir>/pitha-trador/"+settings.FileName+")")
	flag.Parse()

	path := *configPath
	if path == "" {
		var err error
		if path, err = settings.DefaultPath(); err != nil {
			return err
		}
	}
	cfg, err := settings.Load(path)
	if err != nil {
		// An unreadable or malformed file means "off", same as disabled: do
		// not listen. The error carries no file content.
		slog.Error("paper-results-mcp: settings file unusable; not listening", "error", err)
		return nil
	}
	if !cfg.Enabled {
		slog.Info("paper-results-mcp: disabled; not listening")
		return nil
	}

	dbPath, err := resolveDBPath()
	if err != nil {
		return fmt.Errorf("resolve db path: %w", err)
	}
	capital, err := paperInitialCapital()
	if err != nil {
		// Without risk.yaml only the capital ratio is lost; the yen drawdown
		// is still returned (API spec §3.1).
		slog.Warn("paper-results-mcp: paper capital unavailable; ratio will be null")
		capital = 0
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	srv := paperresultsmcp.New(paperresultsmcp.Options{Settings: cfg, DBPath: dbPath, CapitalJPY: capital})
	return srv.Serve(ctx)
}
