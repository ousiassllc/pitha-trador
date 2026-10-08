// Package broker is the broker-neutral boundary between the market-data
// consumers (market-data job, held-position monitor, symbol cache, ranking
// watch, the system banner, the Risk Engine's health signals) and the
// brokerage API adapter that feeds them (docs/architecture/overview.md §4
// "ブローカーアダプタ", overview/integrations.md §5).
//
// It holds the neutral value types (Quote, SymbolInfo, SessionStatus), the
// interfaces an adapter implements (Session, QuoteSource, StreamFeed,
// SymbolInfoSource, CandidateSource, Health) and the Capabilities that
// describe what the adapter can do. Everything that is specific to one
// broker - field naming quirks such as kabuステーション's swapped Bid/Ask,
// registration slots, error codes, operator guidance - stays inside the
// adapter (the current one is internal/service/marketdata/kabu). Packages
// on this side of the boundary must not import an adapter; depguard
// enforces it (.golangci.yml, rule "broker-neutral-no-adapter").
//
// Order placement (OrderGateway) is intentionally not part of this package
// yet (issue #55).
package broker
