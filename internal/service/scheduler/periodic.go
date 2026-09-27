package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository"
)

// HeartbeatChecker is the internal/service/risk.Engine method
// CheckOperatorHeartbeat calls (FR-RISK-6 read side). An interface here
// keeps this package from depending on internal/service/risk directly
// (package doc.go's layer rule); *risk.Engine implements it directly.
type HeartbeatChecker interface {
	CheckHeartbeatTimeout(ctx context.Context) error
}

// WithHeartbeatChecker enables CheckOperatorHeartbeat's periodic
// operator-heartbeat dead-man's-switch check (functional.md FR-RISK-6,
// architecture/overview.md §10.4). Unset by default.
func WithHeartbeatChecker(checker HeartbeatChecker) Option {
	return func(s *Scheduler) { s.heartbeatChecker = checker }
}

// LogRotator archives log files past their local-retention window
// (non-functional.md §5 "直近30日はローカル保持、それ以前は圧縮アーカイ
// ブする"). An interface here keeps this package from depending on
// internal/logging directly (mirrors HeartbeatChecker's own precedent
// above, package doc.go's layer rule); *logging.Archiver implements it
// directly.
type LogRotator interface {
	Rotate(ctx context.Context) error
}

// WithLogRotator enables Start's daily log-archival cron trigger
// (non-functional.md §5). Unset by default.
func WithLogRotator(rotator LogRotator) Option {
	return func(s *Scheduler) { s.logRotator = rotator }
}

// DefaultOutcomeLabelHorizonsMinutes are the judgment horizons Outcome
// Labeling evaluates each Jev trader decision at (functional.md §4.12,
// docs/architecture/er.md §calibration_outcomes "horizon_minutes: 5/10/20
// 等").
var DefaultOutcomeLabelHorizonsMinutes = []int{5, 10, 20}

// EnqueueOutcomeLabeling enqueues one outcome-labeling job
// (repository.JobQueueOutcomeLabeling) for every Jev trader decision
// whose horizon has elapsed as of now but has no calibration_outcomes
// row yet for that (jev_decision_id, horizon_minutes) pair (functional.md
// FR-CAL-4). It returns the number of jobs enqueued, or (0, nil) if no
// OutcomeLabelSource is configured (WithOutcomeLabelSource) - the same
// deferral Start's own doc comment describes for the 15-30s/5-15s
// cycles.
func (s *Scheduler) EnqueueOutcomeLabeling(ctx context.Context, now time.Time) (int, error) {
	if s.outcomeLabels == nil {
		return 0, nil
	}

	pending, err := s.outcomeLabels.PendingLabels(ctx, DefaultOutcomeLabelHorizonsMinutes, now)
	if err != nil {
		return 0, fmt.Errorf("scheduler: list pending outcome labels: %w", err)
	}

	for _, p := range pending {
		payload, err := json.Marshal(repository.OutcomeLabelJobPayload{JevDecisionID: p.JevDecisionID, HorizonMinutes: p.HorizonMinutes})
		if err != nil {
			return 0, fmt.Errorf("scheduler: marshal outcome-labeling payload for decision %d: %w", p.JevDecisionID, err)
		}
		if _, err := s.jobs.Enqueue(ctx, repository.JobQueueOutcomeLabeling, string(payload), now); err != nil {
			return 0, fmt.Errorf("scheduler: enqueue outcome-labeling job for decision %d (horizon %dm): %w", p.JevDecisionID, p.HorizonMinutes, err)
		}
	}
	return len(pending), nil
}

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
