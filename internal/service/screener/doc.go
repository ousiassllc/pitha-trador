// Package screener implements Fast Screener (requirements/functional.md
// §4.2, architecture/overview.md §4): numeric filters that exclude
// obviously out-of-scope instruments before any Jev API call (FR-FS-1),
// screen_score computation, and top-N candidate selection (FR-FS-2).
// Filter thresholds and score weights come from internal/config's
// StrategyConfig (config/strategy.yaml via internal/config.LoadStrategy),
// which already satisfies FR-FS-3 (env/DB-adjustable without code
// changes) - this package only consumes that configuration, it does not
// load it itself.
package screener
