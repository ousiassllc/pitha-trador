package scheduler

import (
	"context"
	"log/slog"
)

// CheckOperatorHeartbeat calls the configured HeartbeatChecker
// (WithHeartbeatChecker) once (functional.md FR-RISK-6, architecture/
// overview.md §10.4's periodic 立会時間中 check), or does nothing and
// returns nil if none is configured - the same deferral
// EnqueueOutcomeLabeling above already documents for
// WithOutcomeLabelSource.
func (s *Scheduler) CheckOperatorHeartbeat(ctx context.Context) error {
	if s.heartbeatChecker == nil {
		return nil
	}
	return s.heartbeatChecker.CheckHeartbeatTimeout(ctx)
}

// CheckRisk calls the configured RiskMonitor (WithRiskMonitor) once, or
// does nothing and returns nil if none is configured.
func (s *Scheduler) CheckRisk(ctx context.Context) error {
	if s.riskMonitor == nil {
		return nil
	}
	return s.riskMonitor.RunPeriodicChecks(ctx)
}

// AutoResumeKillSwitches calls the configured AutoResumer
// (WithAutoResumer) once, or does nothing and returns nil if none is
// configured.
func (s *Scheduler) AutoResumeKillSwitches(ctx context.Context) error {
	if s.autoResumer == nil {
		return nil
	}
	resumed, err := s.autoResumer.AutoResume(ctx)
	if resumed > 0 {
		slog.Info("scheduler: kill switch auto-resumed", "resolved", resumed)
	}
	return err
}

// RotateLogs calls the configured LogRotator (WithLogRotator) once
// (non-functional.md §5), or does nothing and returns nil if none is
// configured - the same deferral EnqueueOutcomeLabeling/
// CheckOperatorHeartbeat above already document.
func (s *Scheduler) RotateLogs(ctx context.Context) error {
	if s.logRotator == nil {
		return nil
	}
	return s.logRotator.Rotate(ctx)
}

// CheckForUpdate calls the configured UpdateChecker (WithUpdateChecker,
// issue #65) once, or does nothing and returns nil if none is configured
// - the same deferral CheckOperatorHeartbeat/RotateLogs above already
// document.
func (s *Scheduler) CheckForUpdate(ctx context.Context) error {
	if s.updateChecker == nil {
		return nil
	}
	return s.updateChecker.CheckForUpdate(ctx)
}
