// Package maintenance runs the once-a-day housekeeping jobs (database
// backup, data-retention purge, log archival; non-functional.md §3/§5)
// with catch-up semantics.
//
// cmd/desktop is typically open only during the trading day, so a plain
// midnight cron trigger never fires while the process is alive. Runner
// therefore remembers the last successful run date per task (persisted in
// runtime_settings through State) and CatchUp runs every task that has
// not yet succeeded today - at start-up and on every periodic check. A
// failing task is retried after RetryDelay, and the operator is notified
// through Notifier once it has failed FailureNotifyThreshold times in a
// row, so a silently broken backup cannot go unnoticed.
//
// The package depends on nothing but the standard library.
package maintenance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

const (
	// DefaultRetryDelay is how long after a failed run the task is tried
	// again by CatchUp.
	DefaultRetryDelay = 30 * time.Minute
	// FailureNotifyThreshold is the number of consecutive failures at
	// which Notifier is told about the task (once per failure streak).
	FailureNotifyThreshold = 3
	// overdueDays is how many days without a success is logged as a
	// warning by CatchUp before the task runs.
	overdueDays = 2

	dayLayout    = "2006-01-02"
	settingKeyFm = "system.maintenance.%s.last_success_date"
)

// ErrSkipped is what a Task's Run returns when it has nothing to do right
// now because the operator has not enabled it (e.g. no backup directory is
// configured in Settings yet). The run counts neither as a success (the
// task is tried again on the next CatchUp, so enabling it takes effect
// within one check interval) nor as a failure (no retry delay, no
// notification).
var ErrSkipped = errors.New("maintenance: task skipped")

// State persists each task's last successful run date.
// *system.RuntimeSettingsRepository implements it.
type State interface {
	Get(ctx context.Context, key string) (string, bool, error)
	Set(ctx context.Context, key, value string, updatedAt time.Time) error
}

// Notifier is told when a task keeps failing.
type Notifier interface {
	MaintenanceFailed(ctx context.Context, task string, consecutiveFailures int, cause error) error
}

// Task is one housekeeping job. Name is the stable state key; Label is
// the operator-facing description used in notifications.
type Task struct {
	Name  string
	Label string
	Run   func(ctx context.Context) error
}

// Runner executes Tasks with catch-up, retry and failure notification.
type Runner struct {
	state      State
	notifier   Notifier
	retryDelay time.Duration
	now        func() time.Time

	mu       sync.Mutex // serializes runs and guards the maps below
	tasks    []Task
	failures map[string]int
	retryAt  map[string]time.Time
}

// Option configures a Runner.
type Option func(*Runner)

// WithRetryDelay overrides DefaultRetryDelay.
func WithRetryDelay(d time.Duration) Option { return func(r *Runner) { r.retryDelay = d } }

// WithNotifier sets the Notifier told about repeatedly failing tasks.
func WithNotifier(n Notifier) Option { return func(r *Runner) { r.notifier = n } }

// WithClock overrides time.Now (tests).
func WithClock(now func() time.Time) Option { return func(r *Runner) { r.now = now } }

// NewRunner returns a Runner persisting last-success dates in state, or
// only in memory when state is nil.
func NewRunner(state State, opts ...Option) *Runner {
	r := &Runner{
		state:      state,
		retryDelay: DefaultRetryDelay,
		now:        time.Now,
		failures:   make(map[string]int),
		retryAt:    make(map[string]time.Time),
	}
	if r.state == nil {
		r.state = newMemoryState()
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// Add registers task with the runner.
func (r *Runner) Add(task Task) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tasks = append(r.tasks, task)
}

// CatchUp runs every task that has not succeeded today (local time) and
// is not waiting out a retry delay. Tasks run one after another; a
// failing task does not stop the others.
func (r *Runner) CatchUp(ctx context.Context) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, task := range r.tasks {
		if ctx.Err() != nil {
			return
		}
		now := r.now()
		if wait, ok := r.retryAt[task.Name]; ok && now.Before(wait) {
			continue
		}
		last, ok := r.lastSuccess(ctx, task.Name)
		today := now.Format(dayLayout)
		if ok && last == today {
			continue
		}
		if ok {
			r.warnIfOverdue(task, last, now)
		}
		r.run(ctx, task, now)
	}
}

// RunNow runs the named task regardless of whether it already succeeded
// today (used by fixed-time triggers). Unknown names are ignored.
func (r *Runner) RunNow(ctx context.Context, name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, task := range r.tasks {
		if task.Name == name {
			r.run(ctx, task, r.now())
			return
		}
	}
}

// run executes task and records the outcome. r.mu must be held.
func (r *Runner) run(ctx context.Context, task Task, now time.Time) {
	err := task.Run(ctx)
	if errors.Is(err, ErrSkipped) {
		slog.Debug("scheduler: maintenance task skipped", "task", task.Name)
		return
	}
	if err == nil {
		delete(r.failures, task.Name)
		delete(r.retryAt, task.Name)
		r.recordSuccess(ctx, task.Name, now)
		return
	}
	if ctx.Err() != nil {
		return // shutting down: neither a task failure nor worth a retry delay
	}
	r.failures[task.Name]++
	n := r.failures[task.Name]
	r.retryAt[task.Name] = now.Add(r.retryDelay)
	slog.Error("scheduler: maintenance task failed", "task", task.Name, "consecutive_failures", n, "error", err)
	if n == FailureNotifyThreshold && r.notifier != nil {
		if nerr := r.notifier.MaintenanceFailed(ctx, task.Label, n, err); nerr != nil {
			slog.Error("scheduler: maintenance failure notification failed", "task", task.Name, "error", nerr)
		}
	}
}

func (r *Runner) lastSuccess(ctx context.Context, name string) (string, bool) {
	raw, ok, err := r.state.Get(ctx, fmt.Sprintf(settingKeyFm, name))
	if err != nil {
		slog.Warn("scheduler: read maintenance state failed; treating the task as not yet run", "task", name, "error", err)
		return "", false
	}
	if !ok {
		return "", false
	}
	var day string
	if err := json.Unmarshal([]byte(raw), &day); err != nil {
		return "", false
	}
	return day, true
}

func (r *Runner) recordSuccess(ctx context.Context, name string, now time.Time) {
	raw, _ := json.Marshal(now.Format(dayLayout))
	if err := r.state.Set(ctx, fmt.Sprintf(settingKeyFm, name), string(raw), now); err != nil {
		slog.Error("scheduler: persist maintenance state failed; the task will run again on the next check", "task", name, "error", err)
	}
}

func (r *Runner) warnIfOverdue(task Task, last string, now time.Time) {
	lastDay, err := time.ParseInLocation(dayLayout, last, now.Location())
	if err != nil {
		return
	}
	if now.Sub(lastDay) >= overdueDays*24*time.Hour {
		slog.Warn("scheduler: maintenance task overdue, running catch-up", "task", task.Name, "last_success_date", last)
	}
}

// memoryState is the State used when none is configured.
type memoryState struct {
	mu sync.Mutex
	m  map[string]string
}

func newMemoryState() *memoryState { return &memoryState{m: make(map[string]string)} }

func (s *memoryState) Get(_ context.Context, key string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.m[key]
	return v, ok, nil
}

func (s *memoryState) Set(_ context.Context, key, value string, _ time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[key] = value
	return nil
}
