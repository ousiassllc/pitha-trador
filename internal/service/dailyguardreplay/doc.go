// Package dailyguardreplay replays the FR-RISK-8 daily entry-halt rules
// (internal/service/risk/dailyguard.go) over recorded closed positions, to
// compare a day's result with and without the rules (issue #782).
//
// It is a pure, read-only calculation: it reproduces the rules on the
// recorded realized P&L only. The unrealized P&L the live engine adds to the
// day's P&L is not recoverable from position records, so a halt the engine
// would have raised on a floating loss, or a profit-lock peak reached only
// unrealized, is not seen here. The report is a hindsight comparison of the
// recorded trades, not a prediction. What the results mean for the
// thresholds, and the Live default-enable criteria, is the owner's decision
// (issue #782, comment 決定 2026-10-17) and is not made in this package.
//
// It depends only on internal/config/dailyguardsettings (the thresholds) and
// internal/domain (the halt reason names); it never touches the risk engine
// or a database.
package dailyguardreplay
