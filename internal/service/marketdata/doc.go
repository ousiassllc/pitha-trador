// Package marketdata is the kabuステーションAPI client (docs/architecture/
// overview.md §5, §4 "Market Data Client"). It issues and holds the API
// token in memory only, registers the scan universe for PUSH updates,
// polls the REST 時価情報・板情報 endpoint, rate-limits information and
// register REST calls process-wide (infolimit; issue #514), and tracks
// per-symbol freshness so callers can treat unresponsive/erroring
// symbols as stale data.
//
// This package MUST depend only on internal/domain, internal/repository,
// and its own subpackages (infolimit). It MUST NOT import sibling
// internal/service sub-packages (e.g. featureengine, scheduler) so those
// packages stay testable and replaceable in isolation.
package marketdata
