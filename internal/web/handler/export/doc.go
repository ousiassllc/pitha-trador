// Package export serves GET /api/v1/export (issue #773): the Settings
// screen's データエクスポート form downloads a ZIP of the trade log,
// decision log and measurement log for a period (internal/service/dataexport,
// requirements/functional/components-platform.md §4.21).
// It depends on internal/service/dataexport and web/middleware; it MUST NOT
// import sibling handler subpackages.
package export
