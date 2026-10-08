// Package marketdata is the kabuステーションAPI client (docs/architecture/
// overview/integrations.md §5.2), wrapped as the broker adapter by
// internal/service/marketdata/kabu (overview.md §4 "ブローカーアダプタ"). It issues and holds the API
// token in memory only, registers the scan universe for PUSH updates,
// polls the REST 時価情報・板情報 endpoint, rate-limits information and
// register REST calls process-wide (infolimit; issue #514), and tracks
// per-symbol freshness so callers can treat unresponsive/erroring
// symbols as stale data.
//
// This package MUST depend only on internal/domain, internal/repository,
// internal/service/broker (the neutral sentinel errors and Health streaks)
// and its own subpackages (infolimit). It MUST NOT import sibling
// internal/service sub-packages (e.g. featureengine, scheduler) so those
// packages stay testable and replaceable in isolation.
package marketdata
