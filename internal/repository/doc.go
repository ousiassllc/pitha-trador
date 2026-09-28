// Package repository provides persistence access (database/sql against the
// embedded SQLite database) for the domain models.
//
// This package MUST depend only on internal/domain. It MUST NOT depend on
// internal/service, internal/router or internal/web. See
// docs/architecture/overview.md §3 for the layer dependency rules
// (handler → service → repository → domain).
//
// The one documented exception is SecretsRepository (secrets_repo.go,
// issue #57), which additionally imports internal/config for its
// AES-256-GCM helpers (config.EncryptSecret/DecryptSecret). config has
// no internal dependencies of its own (its own doc.go), so this does not
// introduce a layering cycle - every other repository here still only
// touches domain.
//
// InstrumentRepository, SnapshotRepository, JobRepository,
// DecisionRepository, SignalRepository, KillSwitchRepository and
// RuntimeSettingsRepository are introduced by earlier sub-scopes and this
// one (instrument_repo.go, snapshot_repo.go, job_repo.go, decision_repo.go,
// signal_repo.go, killswitch_repo.go, runtime_settings_repo.go). The
// remaining concrete repositories (order_repo.go, position_repo.go,
// calibration_repo.go, proposal_repo.go) are introduced by later
// sub-scopes.
package repository
