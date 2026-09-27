package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/ousiassllc/pitha-trador/internal/repository"
)

// defaultPollInterval is how often an idle worker re-polls its queue for
// a newly-due job.
const defaultPollInterval = 200 * time.Millisecond

// Handler processes a single claimed job. A non-nil error marks the job
// failed (repository.JobRepository.MarkFailed); nil marks it succeeded.
type Handler func(ctx context.Context, job repository.Job) error

// fullScanPayload is the JSON body enqueued onto the market-data and
// feature-calc queues once per active instrument, each full-scan cycle
// (functional.md §4.10 FR-SCHED-2 前半).
type fullScanPayload struct {
	InstrumentID int64  `json:"instrument_id"`
	Symbol       string `json:"symbol"`
}

// Scheduler is the jobs-table-backed worker pool + cron-driven full-scan
// trigger described in package doc.go.
type Scheduler struct {
	jobs        *repository.JobRepository
	instruments *repository.InstrumentRepository
	// outcomeLabels is optional (WithOutcomeLabelSource): a nil value
	// makes EnqueueOutcomeLabeling a no-op and skips Start's
	// outcome-labeling trigger.
	outcomeLabels *repository.CalibrationRepository
	// heartbeatChecker is optional (WithHeartbeatChecker): a nil value
	// makes CheckOperatorHeartbeat a no-op, the same deferral
	// outcomeLabels above already documents.
	heartbeatChecker HeartbeatChecker
	// logRotator is optional (WithLogRotator): a nil value makes Start
	// skip registering the @daily log-archival cron trigger entirely
	// (non-functional.md §5 "ログは日次ローテーションし").
	logRotator LogRotator

	pollInterval time.Duration

	mu       sync.Mutex
	handlers map[string]Handler

	cron   *cron.Cron
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// Option configures optional Scheduler behavior.
type Option func(*Scheduler)

// WithPollInterval overrides how often an idle worker re-polls its queue
// (default 200ms).
func WithPollInterval(d time.Duration) Option {
	return func(s *Scheduler) { s.pollInterval = d }
}

// WithOutcomeLabelSource enables EnqueueOutcomeLabeling and Start's
// 1-minute outcome-labeling enqueue trigger (functional.md FR-CAL-4).
// Unset by default. *repository.CalibrationRepository implements this directly.
func WithOutcomeLabelSource(repo *repository.CalibrationRepository) Option {
	return func(s *Scheduler) { s.outcomeLabels = repo }
}

// New returns a Scheduler backed by jobs/instruments.
func New(jobs *repository.JobRepository, instruments *repository.InstrumentRepository, opts ...Option) *Scheduler {
	s := &Scheduler{
		jobs:         jobs,
		instruments:  instruments,
		pollInterval: defaultPollInterval,
		handlers:     make(map[string]Handler),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// RegisterHandler registers the function that processes jobs claimed off
// queue. Call it before Start; only one handler may be registered per
// queue.
func (s *Scheduler) RegisterHandler(queue string, h Handler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers[queue] = h
}

// Recover resets every job left status='running' by a previous crash back
// to pending (er.md §jobs "再起動時の回復"), returning how many rows were
// reset. Call it once at startup, before Start.
func (s *Scheduler) Recover(ctx context.Context) (int64, error) {
	n, err := s.jobs.ResetStuckRunning(ctx)
	if err != nil {
		return 0, fmt.Errorf("scheduler: recover stuck running jobs: %w", err)
	}
	return n, nil
}

// EnqueueFullScan enqueues one market-data job and one feature-calc job
// per active instrument, due at now (functional.md FR-SCHED-2 前半). It
// returns the number of instruments enqueued for, and logs that count as
// a structured JSON line (non-functional.md §5.1 "スキャン対象銘柄数").
func (s *Scheduler) EnqueueFullScan(ctx context.Context, now time.Time) (int, error) {
	instruments, err := s.instruments.ListActive(ctx)
	if err != nil {
		return 0, fmt.Errorf("scheduler: list active instruments for full scan: %w", err)
	}

	for _, inst := range instruments {
		payload, err := json.Marshal(fullScanPayload{InstrumentID: inst.ID, Symbol: inst.Symbol})
		if err != nil {
			return 0, fmt.Errorf("scheduler: marshal full scan payload for %q: %w", inst.Symbol, err)
		}
		if _, err := s.jobs.Enqueue(ctx, repository.JobQueueMarketData, string(payload), now); err != nil {
			return 0, fmt.Errorf("scheduler: enqueue market-data job for %q: %w", inst.Symbol, err)
		}
		if _, err := s.jobs.Enqueue(ctx, repository.JobQueueFeatureCalc, string(payload), now); err != nil {
			return 0, fmt.Errorf("scheduler: enqueue feature-calc job for %q: %w", inst.Symbol, err)
		}
	}
	slog.Info("scheduler: full scan enqueued", "instrument_count", len(instruments))
	return len(instruments), nil
}

// EnqueueEventReevaluation enqueues one jev-scout job for instrumentID,
// due immediately at now, when triggered is true - bypassing the normal
// 15-30s candidate-refresh cadence for a symbol whose
// featureengine.DetectEvent signal fired (FR-SCAN-1). When triggered is
// false it does nothing, leaving the Jev call for this cycle skipped
// (FR-SCAN-2 quiet-suppression): the caller (featureengine.EventSignal.
// Triggered against config/strategy.yaml's scan.event_trigger
// thresholds, wired in by a later sub-scope alongside Fast Screener's own
// periodic candidate-refresh enqueue - this package cannot import
// internal/service/featureengine per doc.go's layer rule) decides
// triggered.
func (s *Scheduler) EnqueueEventReevaluation(ctx context.Context, instrumentID int64, symbol string, triggered bool, now time.Time) error {
	if !triggered {
		return nil
	}
	payload, err := json.Marshal(fullScanPayload{InstrumentID: instrumentID, Symbol: symbol})
	if err != nil {
		return fmt.Errorf("scheduler: marshal event-driven reevaluation payload for %q: %w", symbol, err)
	}
	if _, err := s.jobs.Enqueue(ctx, repository.JobQueueJevScout, string(payload), now); err != nil {
		return fmt.Errorf("scheduler: enqueue event-driven jev-scout job for %q: %w", symbol, err)
	}
	return nil
}

// Start launches one worker goroutine per registered handler's queue plus
// the cron-driven full-scan trigger at fullScanInterval
// (functional.md §4.3), the daily selfImproveCronSpec Sol-analysis
// trigger (functional.md §4.14 FR-SELFIMPROVE-1) and - when
// WithOutcomeLabelSource/WithLogRotator were given - the 1-minute
// Outcome Labeling enqueue trigger (FR-CAL-4) and a @daily log-archival
// trigger (non-functional.md §5), running until ctx is done or Stop is
// called.
//
// Only the 60-second full-scan cycle is wired to an actual enqueue here;
// the 15-30s candidate-refresh and 5-15s held-position cycles
// (functional.md §4.3) have no jobs to enqueue until the Fast Screener and
// Jev Scout scopes exist, so their trigger registration is deferred to
// those scopes rather than registering an empty placeholder here.
func (s *Scheduler) Start(ctx context.Context, fullScanInterval time.Duration) error {
	runCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel

	s.mu.Lock()
	queues := make([]string, 0, len(s.handlers))
	for q := range s.handlers {
		queues = append(queues, q)
	}
	s.mu.Unlock()

	for _, q := range queues {
		s.wg.Add(1)
		go s.runWorker(runCtx, q)
	}

	s.cron = cron.New()
	spec := fmt.Sprintf("@every %s", fullScanInterval)
	if _, err := s.cron.AddFunc(spec, func() {
		if _, err := s.EnqueueFullScan(runCtx, time.Now().UTC()); err != nil {
			slog.Error("scheduler: full scan enqueue failed", "error", err)
		}
	}); err != nil {
		cancel()
		return fmt.Errorf("scheduler: register full scan trigger %q: %w", spec, err)
	}

	if _, err := s.cron.AddFunc(selfImproveCronSpec, func() {
		if err := s.EnqueueSelfImprove(runCtx, time.Now().UTC()); err != nil {
			slog.Error("scheduler: self-improve enqueue failed", "error", err)
		}
	}); err != nil {
		cancel()
		return fmt.Errorf("scheduler: register self-improve trigger %q: %w", selfImproveCronSpec, err)
	}

	if err := s.addPeriodicTriggers(runCtx); err != nil {
		cancel()
		return err
	}

	s.cron.Start()

	return nil
}

// Stop cancels every running worker goroutine and the cron trigger,
// blocking until the workers have exited.
func (s *Scheduler) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
	if s.cron != nil {
		<-s.cron.Stop().Done()
	}
	s.wg.Wait()
}

func (s *Scheduler) runWorker(ctx context.Context, queue string) {
	defer s.wg.Done()

	s.mu.Lock()
	handler := s.handlers[queue]
	s.mu.Unlock()

	ticker := time.NewTicker(s.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.processNext(ctx, queue, handler)
		}
	}
}

func (s *Scheduler) processNext(ctx context.Context, queue string, handler Handler) {
	job, err := s.jobs.ClaimNext(ctx, queue, time.Now().UTC())
	if err != nil {
		if !errors.Is(err, repository.ErrJobNotFound) {
			slog.Error("scheduler: claim job failed", "queue", queue, "error", err)
		}
		return
	}

	if err := handler(ctx, job); err != nil {
		if markErr := s.jobs.MarkFailed(ctx, job.ID, time.Now().UTC(), err.Error()); markErr != nil {
			slog.Error("scheduler: mark job failed", "job_id", job.ID, "error", markErr)
		}
		return
	}

	if err := s.jobs.MarkSucceeded(ctx, job.ID, time.Now().UTC()); err != nil {
		slog.Error("scheduler: mark job succeeded", "job_id", job.ID, "error", err)
	}
}
