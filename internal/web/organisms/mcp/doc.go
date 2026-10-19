// Package mcp renders the Settings screen's MCP section (issue #792,
// docs/components/overview.md §3 "McpPanel"): the state, enable switch,
// listen address, API key action, the values to register in the Grok Bot
// and where paper-results-mcp and its settings file live. It takes plain,
// already formatted props (internal/web/handler/mcpsettings maps the admin
// service's view onto them), so it never sees internal/service. It is its
// own package because internal/web/organisms is over its line limit.
package mcp
