// Package organisms holds Atomic Design organisms (Header, KillSwitchPanel,
// ScannerTableFallback, DecisionHistoryList, PerformanceSummaryPanel, ...)
// composed from internal/web/molecules and internal/web/atoms. See
// docs/components/overview.md §3.
//
// Sidebar/CalibrationBucketTable are deliberately absent: navigation is
// Header's and the Calibration view is pitha-calibration-heatmap's (issue
// #120), so each is added here only when a page needs it.
package organisms
