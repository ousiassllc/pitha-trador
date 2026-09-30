// Package system persists application-level state: kill_switch_events
// (KillSwitchRepository, killswitch_repo.go), runtime_settings
// (RuntimeSettingsRepository, runtime_settings_repo.go) and secrets
// (SecretsRepository, secrets_repo.go).
//
// It depends on internal/domain and internal/repository/sqlutil. The one
// documented exception (issue #57) is SecretsRepository, which additionally
// imports internal/config for its AES-256-GCM helpers
// (config.EncryptSecret/DecryptSecret); config has no internal dependencies
// of its own (its own doc.go), so this does not introduce a layering cycle.
// The exception stays inside this package. See
// docs/architecture/overview.md §3.
package system
