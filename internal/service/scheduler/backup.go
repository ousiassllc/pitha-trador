package scheduler

import "context"

// DatabaseBackuper copies the SQLite database to external storage and
// prunes old copies (non-functional.md §3 "DBファイルを日次でバックアップ").
// An interface here keeps this package from depending on
// internal/service/backup directly (mirrors LogRotator's own precedent
// in periodic.go); *backup.Service implements it directly.
type DatabaseBackuper interface {
	Backup(ctx context.Context) error
}

// WithDatabaseBackuper enables Start's @daily database-backup cron
// trigger (non-functional.md §3). Unset by default.
func WithDatabaseBackuper(backuper DatabaseBackuper) Option {
	return func(s *Scheduler) { s.databaseBackuper = backuper }
}

// BackupDatabase calls the configured DatabaseBackuper
// (WithDatabaseBackuper) once (non-functional.md §3), or does nothing and
// returns nil if none is configured - the same deferral RotateLogs
// (periodic.go) documents.
func (s *Scheduler) BackupDatabase(ctx context.Context) error {
	if s.databaseBackuper == nil {
		return nil
	}
	return s.databaseBackuper.Backup(ctx)
}
