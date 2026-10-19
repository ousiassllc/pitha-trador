// Package admin is the application side of the read-only Paper results MCP's
// configuration (issue #792): the Settings screen's switch, listen address
// and API key, kept in the MCP's own settings file (package settings) and
// never in the database, a log line, an export or a backup.
//
// Since issue #795 it also starts, stops and restarts the MCP process, by
// delegating to package proc (the process stays separate and read-only,
// docs/architecture/paper-results-mcp.md §3.4; the key is never held here or
// passed to it). The state it reports also comes from a TCP connect to the
// configured address, which tells whether something is listening there.
package admin
