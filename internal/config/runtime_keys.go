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
