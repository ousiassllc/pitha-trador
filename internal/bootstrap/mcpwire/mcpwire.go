// Package mcpwire builds the Settings screen's MCP service (issues #792,
// #795): the card edits the read-only Paper results MCP's own settings file,
// and the same service starts the MCP process with the application and stops
// it when the application exits.
package mcpwire

import (
	"context"
	"log/slog"

	"github.com/ousiassllc/pitha-trador/internal/router"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/admin"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/settings"
)

// New returns the MCP service over the MCP's default settings file
// (settings.DefaultPath). Without a resolvable per-user config directory the
// MCP process could not find its file either, so it logs a warning and returns
// nil: the card stays hidden and no process is ever started.
func New() *admin.Service {
	path, err := settings.DefaultPath()
	if err != nil {
		slog.Warn("mcpwire: MCP settings path unavailable; the Settings screen's MCP card is hidden", "error", err)
		return nil
	}
	return admin.New(admin.Options{Path: path})
}

// RouterOptions returns router.WithMCPSettings over svc, or no option for a
// nil svc (the card is hidden). Entrypoints append it to bootstrap.RouterOptions.
func RouterOptions(svc *admin.Service) []router.Option {
	if svc == nil {
		return nil
	}
	return []router.Option{router.WithMCPSettings(svc)}
}

// Start starts the MCP process with the application when its stored settings
// are enabled, hold an API key and a permitted address, and does nothing
// otherwise (issue #795). The entrypoints call it once the application's own
// background services are running; it returns at once (the hand-over after a
// supervisor restart may wait a few seconds for the previous process's port
// to close) and gives up when ctx is canceled. A nil svc is a no-op.
func Start(ctx context.Context, svc *admin.Service) {
	if svc != nil {
		go svc.AutoStart(ctx)
	}
}

// Stop ends the MCP process and refuses any later start. The entrypoints call
// it first when the application exits. A crash of the application is covered
// by the operating system instead (a Windows Job Object,
// internal/service/paperresultsmcp/proc). A nil svc is a no-op.
func Stop(svc *admin.Service) {
	if svc != nil {
		svc.Shutdown()
	}
}
