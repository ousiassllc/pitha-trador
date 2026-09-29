package scheduler

import "context"

// DataPurger deletes rows past their retention window from the
// high-frequency tables (jobs, market_snapshots; non-functional.md §3
// "データ保持"). An interface here keeps this package from depending on
// internal/service/retention directly (mirrors LogRotator's precedent in
// periodic.go); *retention.Service implements it directly.
type DataPurger interface {
	Purge(ctx context.Context) error
}

// WithDataPurger enables Start's daily data-retention purge (catch-up) trigger
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
