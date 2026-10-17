// Package actuals reads the closed Paper trades, the Jev decision gradings and
// the meta-labeling models for the realized-performance evaluation (issue
// #777, internal/service/actuals). It is strictly read-only: it never writes
// to the trading, settings or market tables, and its row types carry no raw
// market data (board/quote values).
//
// It joins positions, paper_orders, trade_signals, jev_decisions,
// calibration_outcomes and metalabel_* across resource groups, so it is its
// own package (as internal/repository/decisiontrade is) and MUST depend only
// on internal/repository/sqlutil. See docs/architecture/overview.md §3.
//
// The row types live here, not in internal/domain: that directory is over
// the 2000-line limit enforced by linterly.
package actuals
