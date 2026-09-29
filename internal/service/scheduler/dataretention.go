package scheduler

import (
	"context"
	"fmt"
	"log/slog"
)

// DataPurger deletes rows past their retention window from the
// high-frequency tables (jobs, market_snapshots; non-functional.md §3
// "データ保持"). An interface here keeps this package from depending on
// internal/service/retention directly (mirrors LogRotator's precedent in
// periodic.go); *retention.Service implements it directly.
type DataPurger interface {
	Purge(ctx context.Context) error
}

// WithDataPurger enables Start's @daily data-retention purge trigger
// (non-functional.md §3). Unset by default.
func WithDataPurger(purger DataPurger) Option {
	return func(s *Scheduler) { s.dataPurger = purger }
}

// PurgeExpiredData calls the configured DataPurger (WithDataPurger) once,
// or does nothing and returns nil if none is configured - the same
// deferral RotateLogs (periodic.go) documents.
func (s *Scheduler) PurgeExpiredData(ctx context.Context) error {
	if s.dataPurger == nil {
		return nil
	}
	return s.dataPurger.Purge(ctx)
}

// addDataMaintenanceTriggers registers the @daily database-backup and
// data-retention purge triggers, each only when its Option configured the
// dependency it drives.
func (s *Scheduler) addDataMaintenanceTriggers(ctx context.Context) error {
	if s.databaseBackuper != nil {
		if _, err := s.cron.AddFunc("@daily", func() {
			if err := s.BackupDatabase(ctx); err != nil {
				slog.Error("scheduler: database backup failed", "error", err)
			}
		}); err != nil {
			return fmt.Errorf("scheduler: register database backup trigger: %w", err)
		}
	}
	if s.dataPurger != nil {
		if _, err := s.cron.AddFunc("@daily", func() {
			if err := s.PurgeExpiredData(ctx); err != nil {
				slog.Error("scheduler: data retention purge failed", "error", err)
			}
		}); err != nil {
			return fmt.Errorf("scheduler: register data retention purge trigger: %w", err)
		}
	}
	return nil
}
