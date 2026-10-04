// Package organisms holds Atomic Design organisms (Header, KillSwitchPanel,
// ScannerTableFallback, DecisionHistoryList, PerformanceSummaryPanel, ...)
// composed from internal/web/molecules and internal/web/atoms. See
// docs/components/overview.md §3.
//
// Dependency policy: organisms (like internal/web/pages) may import
// internal/domain but never internal/service. They take plain props
// (PerformanceSummary, PerformanceActuals, UpdateBannerProps, ...) that
// internal/web/handler fills from the service result types, so a service
// type change cannot break the templates. The golangci-lint depguard rule
// `templ-no-service` enforces it.
//
// Sidebar/CalibrationBucketTable are deliberately absent: navigation is
// Header's and the Calibration view is pitha-calibration-heatmap's (issue
// #120), so each is added here only when a page needs it.
package organisms
