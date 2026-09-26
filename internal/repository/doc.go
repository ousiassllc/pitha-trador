// Package repository provides persistence access (database/sql against the
// embedded SQLite database) for the domain models.
//
// This package MUST depend only on internal/domain. It MUST NOT depend on
// internal/service, internal/router or internal/web. See
// docs/architecture/overview.md §3 for the layer dependency rules
// (handler → service → repository → domain).
//
// InstrumentRepository, SnapshotRepository and JobRepository are
// introduced by this sub-scope (instrument_repo.go, snapshot_repo.go,
// job_repo.go). The remaining concrete repositories (decision_repo.go,
// signal_repo.go, order_repo.go, position_repo.go, calibration_repo.go,
// proposal_repo.go) are introduced by later sub-scopes.
package repository
