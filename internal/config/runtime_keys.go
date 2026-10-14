package config

// runtime_settings keys for the operator-editable path settings of the
// Settings screen (issue #708; docs/architecture/er.md §runtime_settings).
// Values are JSON strings; a missing row means "not configured".
const (
	// KeyBackupDir is the destination directory of the daily SQLite backup
	// (requirements/non-functional.md §3). Unset disables the backup.
	KeyBackupDir = "system.backup_dir"
	// KeyLogDir overrides the daily JSON log / error-log export directory
	// (requirements/non-functional.md §5). Unset keeps "logs" next to the
	// SQLite DB. Read once at process start.
	KeyLogDir = "system.log_dir"
)

// runtime_settings keys for the log level and the local log retention
// (issue #772; requirements/non-functional.md §5.2). Parsing, bounds and
// defaults live in internal/config/logsettings.
const (
	// KeyLogLevel is the minimum level of the daily JSON log: a JSON string,
	// "debug" | "info" | "warn" | "error". Unset keeps "info", the level that
	// carries the metrics records the metrics export reads. Read once at
	// process start.
	KeyLogLevel = "system.log_level"
	// KeyLogRetentionDays is how many days of daily log files stay
	// uncompressed before the Archiver gzips them: a JSON integer. Unset keeps
	// 30. Read on every archival pass, so no restart is needed.
	KeyLogRetentionDays = "system.log_retention_days"
)
