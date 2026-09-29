package scheduler

import (
	"context"
	"fmt"

	"github.com/ousiassllc/pitha-trador/internal/service/scheduler/maintenance"
)

const (
	// maintenanceCatchUpCronSpec is how often Start's maintenance trigger
	// checks for daily tasks that have not yet succeeded today. cmd/desktop
	// usually runs only during the trading day, so a midnight-only cron
	// (issue #152) would never fire; catching up on start plus this
	// periodic check does.
	maintenanceCatchUpCronSpec = "@every 10m"
	// backupEndOfDayCronSpec additionally runs the database backup at 16:00
	// local time (after the close), so the day's copy holds the day's data
	// rather than only the state at the morning catch-up.
	backupEndOfDayCronSpec = "0 16 * * *"

	taskDatabaseBackup = "database_backup"
	taskDataRetention  = "data_retention_purge"
	taskLogRotation    = "log_rotation"
)

// WithMaintenanceState persists the daily maintenance tasks' (database
// backup, retention purge, log archival) last success date, so a restart
// does not repeat a task that already succeeded today.
// *repository.RuntimeSettingsRepository implements it. Without it the
// dates are only kept in memory.
func WithMaintenanceState(state maintenance.State) Option {
	return func(s *Scheduler) { s.maintenanceState = state }
}

// WithMaintenanceNotifier sets the notifier told when a maintenance task
// has failed several times in a row (e.g. an unmounted backup drive).
func WithMaintenanceNotifier(notifier maintenance.Notifier) Option {
	return func(s *Scheduler) { s.maintenanceNotifier = notifier }
}

// addMaintenanceTriggers registers the daily database-backup,
// data-retention purge and log-archival tasks, each only when its Option
// configured the dependency it drives. Each runs once per day with
// catch-up (see package maintenance): immediately after Start and then on
// every maintenanceCatchUpCronSpec tick until it has succeeded today.
func (s *Scheduler) addMaintenanceTriggers(ctx context.Context) error {
	var opts []maintenance.Option
	if s.maintenanceNotifier != nil {
		opts = append(opts, maintenance.WithNotifier(s.maintenanceNotifier))
	}
	runner := maintenance.NewRunner(s.maintenanceState, opts...)
	if s.databaseBackuper != nil {
		runner.Add(maintenance.Task{Name: taskDatabaseBackup, Label: "DBバックアップ", Run: s.BackupDatabase})
	}
	if s.dataPurger != nil {
		runner.Add(maintenance.Task{Name: taskDataRetention, Label: "データ保持パージ", Run: s.PurgeExpiredData})
	}
	if s.logRotator != nil {
		runner.Add(maintenance.Task{Name: taskLogRotation, Label: "ログアーカイブ", Run: s.RotateLogs})
	}
	if s.databaseBackuper == nil && s.dataPurger == nil && s.logRotator == nil {
		return nil
	}

	if _, err := s.cron.AddFunc(maintenanceCatchUpCronSpec, func() { runner.CatchUp(ctx) }); err != nil {
		return fmt.Errorf("scheduler: register maintenance catch-up trigger: %w", err)
	}
	if s.databaseBackuper != nil {
		if _, err := s.cron.AddFunc(backupEndOfDayCronSpec, func() { runner.RunNow(ctx, taskDatabaseBackup) }); err != nil {
			return fmt.Errorf("scheduler: register end-of-day backup trigger: %w", err)
		}
	}

	// Catch up immediately (async, tracked on s.wg so Stop waits for it):
	// the process may start after midnight and close before the next tick.
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer recoverPanic("maintenance catch-up")
		runner.CatchUp(ctx)
	}()
	return nil
}
