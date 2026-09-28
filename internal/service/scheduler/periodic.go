package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
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

// WithHeartbeatChecker enables Start's 1-minute CheckOperatorHeartbeat
// trigger, the operator-heartbeat dead-man's-switch check (functional.md
// FR-RISK-6, architecture/overview.md §10.4). Unset by default.
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

// UpdateChecker is internal/service/updater's periodic entrypoint
// (issue #65's automatic GitHub Releases update check). An interface
// here keeps this package from depending on internal/service/updater
// directly (mirrors HeartbeatChecker/LogRotator's own precedent above,
// package doc.go's layer rule); internal/bootstrap's cmd/desktop-only
// adapter wraps *updater.Checker.CheckForUpdate plus the Wails-specific
// quit-and-relaunch trigger neither this package nor
// internal/service/updater may import.
type UpdateChecker interface {
	CheckForUpdate(ctx context.Context) error
}

// WithUpdateChecker enables Start's GitHub Releases update-check
// trigger (issue #65): once immediately when Start is called, then
// every 6 hours after (issue #71 - robfig/cron/v3's "@every 6h" alone
// only fires 6h after Start, never on Start itself). cmd/desktop only;
// cmd/server never configures this (it has no installer to run). Unset
// by default.
func WithUpdateChecker(checker UpdateChecker) Option {
	return func(s *Scheduler) { s.updateChecker = checker }
}

// outcomeLabelingCronSpec is how often Start's Outcome Labeling trigger
// (WithOutcomeLabelSource) runs EnqueueOutcomeLabeling: once per 1-minute
// market_snapshots bar, so each 5/10/20-minute horizon is labeled within
// a minute of elapsing.
const outcomeLabelingCronSpec = "@every 1m"

// heartbeatCheckCronSpec is how often Start's operator-heartbeat trigger
// (WithHeartbeatChecker) runs CheckOperatorHeartbeat: once a minute, the
// finest granularity risk.yaml's heartbeat_timeout_minutes is expressed
// in (architecture/overview.md §10.4's periodic check).
const heartbeatCheckCronSpec = "@every 1m"

// updateCheckCronSpec is how often Start's WithUpdateChecker trigger
// (issue #65) polls GitHub Releases for a newer version: every 6 hours,
// far coarser than every other trigger here since a new release is a
// rare, human-paced event rather than a market-data-paced one.
const updateCheckCronSpec = "@every 6h"

// outcomeLabelRetryWindow bounds how long after its decision a pending
// (decision, horizon) pair keeps being re-enqueued. Labeler.HandleJob
// fails (persisting nothing) when the decision's horizon window has no
// market data, and a gap that old (kabuステーションAPI outage, process
// downtime) never fills in, so without this bound every such pair would
// be re-enqueued - and fail - every minute forever.
const outcomeLabelRetryWindow = 24 * time.Hour

// DefaultOutcomeLabelHorizonsMinutes are the judgment horizons Outcome
// Labeling evaluates each Jev trader decision at (functional.md §4.12,
// docs/architecture/er.md §calibration_outcomes "horizon_minutes: 5/10/20
// 等").
var DefaultOutcomeLabelHorizonsMinutes = []int{5, 10, 20}

// EnqueueOutcomeLabeling enqueues one outcome-labeling job
// (repository.JobQueueOutcomeLabeling) for every Jev trader decision
// whose horizon has elapsed as of now but has no calibration_outcomes
// row yet for that (jev_decision_id, horizon_minutes) pair (functional.md
// FR-CAL-4), skipping decisions older than outcomeLabelRetryWindow. It
// returns the number of jobs enqueued, or (0, nil) if no
// OutcomeLabelSource is configured (WithOutcomeLabelSource).
func (s *Scheduler) EnqueueOutcomeLabeling(ctx context.Context, now time.Time) (int, error) {
	if s.outcomeLabels == nil {
		return 0, nil
	}

	pending, err := s.outcomeLabels.PendingLabels(ctx, DefaultOutcomeLabelHorizonsMinutes, now)
	if err != nil {
		return 0, fmt.Errorf("scheduler: list pending outcome labels: %w", err)
	}

	enqueued := 0
	for _, p := range pending {
		if p.DecisionTimestamp.Before(now.Add(-outcomeLabelRetryWindow)) {
			continue
		}
		payload, err := json.Marshal(repository.OutcomeLabelJobPayload{JevDecisionID: p.JevDecisionID, HorizonMinutes: p.HorizonMinutes})
		if err != nil {
			return 0, fmt.Errorf("scheduler: marshal outcome-labeling payload for decision %d: %w", p.JevDecisionID, err)
		}
		if _, err := s.jobs.Enqueue(ctx, repository.JobQueueOutcomeLabeling, string(payload), now); err != nil {
			return 0, fmt.Errorf("scheduler: enqueue outcome-labeling job for decision %d (horizon %dm): %w", p.JevDecisionID, p.HorizonMinutes, err)
		}
		enqueued++
	}
	return enqueued, nil
}

// addPeriodicTriggers registers Start's optional cron triggers - each
// only when its Option configured the dependency it drives.
func (s *Scheduler) addPeriodicTriggers(ctx context.Context) error {
	if s.outcomeLabels != nil {
		if _, err := s.cron.AddFunc(outcomeLabelingCronSpec, func() {
			if _, err := s.EnqueueOutcomeLabeling(ctx, time.Now().UTC()); err != nil {
				slog.Error("scheduler: outcome-labeling enqueue failed", "error", err)
			}
		}); err != nil {
			return fmt.Errorf("scheduler: register outcome-labeling trigger: %w", err)
		}
	}
	if s.heartbeatChecker != nil {
		if _, err := s.cron.AddFunc(heartbeatCheckCronSpec, func() {
			if err := s.CheckOperatorHeartbeat(ctx); err != nil {
				slog.Error("scheduler: operator heartbeat check failed", "error", err)
			}
		}); err != nil {
			return fmt.Errorf("scheduler: register operator heartbeat trigger: %w", err)
		}
	}
	if s.logRotator != nil {
		if _, err := s.cron.AddFunc("@daily", func() {
			if err := s.RotateLogs(ctx); err != nil {
				slog.Error("scheduler: log rotation failed", "error", err)
			}
		}); err != nil {
			return fmt.Errorf("scheduler: register log rotation trigger: %w", err)
		}
	}
	if s.updateChecker != nil {
		checkForUpdate := func() {
			if err := s.CheckForUpdate(ctx); err != nil {
				slog.Error("scheduler: update check failed", "error", err)
			}
		}
		if _, err := s.cron.AddFunc(updateCheckCronSpec, checkForUpdate); err != nil {
			return fmt.Errorf("scheduler: register update-check trigger: %w", err)
		}
		// robfig/cron/v3's "@every 6h" (updateCheckCronSpec) computes its
		// first Next(now) as now.Add(6h), never now itself (issue #71),
		// so without this immediate run a process whose lifetime never
		// reaches 6h - cmd/desktop's typical intraday restart cadence -
		// would never perform a single update check. Runs async (not
		// inline here) since CheckForUpdate downloads+verifies an
		// installer on a newer release and must not delay Start/the
		// caller's startup sequence. Tracked on s.wg (like runWorker's
		// goroutines) so Stop's `s.wg.Wait()` blocks until this check has
		// actually returned, instead of Stop reporting done while an
		// installer download/verification is still in flight.
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			checkForUpdate()
		}()
	}
	return nil
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
