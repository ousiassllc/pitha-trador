// Package mcpwire wires the Settings screen's MCP card (issue #792) into the
// router: the card edits the read-only Paper results MCP's own settings file.
package mcpwire

import (
	"log/slog"

	"github.com/ousiassllc/pitha-trador/internal/router"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/admin"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/settings"
)

// RouterOptions returns router.WithMCPSettings over the MCP's default settings
// file (settings.DefaultPath). Without a resolvable per-user config directory
// the MCP process could not find its file either, so it logs a warning and
// returns no option: the card stays hidden.
func RouterOptions() []router.Option {
	path, err := settings.DefaultPath()
	if err != nil {
		slog.Warn("mcpwire: MCP settings path unavailable; the Settings screen's MCP card is hidden", "error", err)
		return nil
	}
	return []router.Option{router.WithMCPSettings(admin.New(admin.Options{Path: path}))}
}
