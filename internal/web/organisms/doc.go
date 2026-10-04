// Package organisms holds Atomic Design organisms composed from
// internal/web/molecules and internal/web/atoms. The list of organisms is
// kept only in docs/components/overview.md §3 (not duplicated here).
//
// Dependency policy: organisms (like internal/web/pages) may import
// internal/domain but never internal/service. They take plain props
// (PerformanceSummary, PerformanceActuals, UpdateBannerProps, ...) that
// internal/web/handler fills from the service result types, so a service
// type change cannot break the templates. The golangci-lint depguard rule
// `templ-no-service` enforces it.
//
// Components with no consumer are deliberately absent (issue #120); each is
// added here only when a page needs it (see overview.md §3).
package organisms
