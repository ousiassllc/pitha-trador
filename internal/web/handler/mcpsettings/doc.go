// Package mcpsettings serves the Settings screen's MCP section (issue #792):
// `GET /settings/mcp-panel` (the lazily loaded panel), `POST /settings/mcp`
// (switch and listen address) and `POST /settings/mcp/key` (generate /
// regenerate the API key, shown once). The API key lives only in the MCP's
// own 0600 settings file (internal/service/paperresultsmcp/settings); this
// package sees it only as the return value of the key action.
package mcpsettings
