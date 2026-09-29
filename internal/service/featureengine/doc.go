// Package featureengine computes the price/VWAP/volume/volatility/
// orderbook/market-context feature values Feature Engine is responsible
// for (docs/requirements/functional.md §4.1, docs/architecture/overview.md
// §4 "Feature Engine") and persists them to market_snapshots.
//
// All computation happens in Compute, a pure function over a single
// instrument's current market data reading plus its prior bars: every
// derived value uses only data timestamped at or before the bar being
// computed (FR-FE-1 look-ahead防止), and board/orderbook-derived values
// are nil whenever the current reading lacks bid/ask/quantity data
// (FR-FE-2 欠損値扱い). Engine wraps Compute with persistence, writing one
// scan cycle's worth of computed snapshots to market_snapshots inside a
// single transaction (docs/architecture/er.md §market_snapshots "運用上の
// 注意"). Its eventtrigger subpackage's Detect compares two consecutive cycles' Snapshots (plus
// prior bars for the high/low breakout check) to derive the Signal
// internal/service/scheduler.EnqueueEventReevaluation's caller decides
// FR-SCAN-1's immediate re-evaluation / FR-SCAN-2's quiet-suppression
// trigger from.
//
// This package MUST depend only on internal/domain and internal/repository
// (internal/service/doc.go); it does not import internal/service/marketdata
// so it stays independently testable. Callers translate
// marketdata.Board into the featureengine.Reading this package expects.
package featureengine
