// Package admin is the application side of the read-only Paper results MCP's
// configuration (issue #792): the Settings screen's switch, listen address
// and API key, kept in the MCP's own settings file (package settings) and
// never in the database, a log line, an export or a backup.
//
// It does not start, stop or talk to the MCP process (the process stays
// separate and read-only, docs/architecture/paper-results-mcp.md §3.4). The
// only contact is a TCP connect to the configured address to tell whether
// something is listening there.
package admin
