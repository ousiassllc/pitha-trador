// Package opsettings is the Settings screen's store for operator-editable
// settings that are not secrets (issue #708): the backup / log directories
// (formerly PITHA_BACKUP_DIR / PITHA_LOG_DIR) and the Policy Engine /
// Fast Screener thresholds (formerly PITHA_POLICY_* / PITHA_FAST_SCREENER_*).
// They live in the runtime_settings table under the keys config.KeyBackupDir,
// config.KeyLogDir, config.PolicySettingKeys and
// config.FastScreenerSettingKeys.
//
// Priority is config/strategy.yaml < runtime_settings: Get reports the
// effective value (the stored row, else the yaml value), Save validates and
// stores a row, Reset deletes it so the yaml value applies again. The
// consumers (Policy Engine, Fast Screener refresh, backup job) re-read
// runtime_settings on every use, except the log directory, which is read
// once when the process starts.
package opsettings
