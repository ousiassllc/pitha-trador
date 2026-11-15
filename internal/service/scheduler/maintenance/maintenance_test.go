package maintenance

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeNotifier struct {
	calls []int
	tasks []string
}

func (f *fakeNotifier) MaintenanceFailed(_ context.Context, task string, n int, _ error) error {
	f.tasks = append(f.tasks, task)
	f.calls = append(f.calls, n)
	return nil
}

type counter struct {
	calls int
	err   error
}

func (c *counter) run(context.Context) error { c.calls++; return c.err }

func fixedClock(t *time.Time) func() time.Time { return func() time.Time { return *t } }

func TestCatchUp_RunsTaskNotYetSucceededTodayThenOncePerDay(t *testing.T) {
	now := time.Date(2026, 9, 29, 9, 0, 0, 0, time.Local)
	state := newMemoryState()
	// Last success was yesterday: the process never lived through 00:00.
	_ = state.Set(context.Background(), "system.maintenance.backup.last_success_date", `"2026-09-28"`, now)

	c := &counter{}
	r := NewRunner(state, WithClock(fixedClock(&now)))
	r.Add(Task{Name: "backup", Label: "backup", Run: c.run})

	r.CatchUp(context.Background())
	if c.calls != 1 {
		t.Fatalf("calls after first CatchUp = %d, want 1", c.calls)
	}
	got, _, _ := state.Get(context.Background(), "system.maintenance.backup.last_success_date")
	if got != `"2026-09-29"` {
		t.Fatalf("persisted date = %s, want \"2026-09-29\"", got)
	}

	now = now.Add(6 * time.Hour)
	r.CatchUp(context.Background())
	if c.calls != 1 {
		t.Fatalf("calls after second CatchUp same day = %d, want 1", c.calls)
	}

	now = now.Add(24 * time.Hour)
	r.CatchUp(context.Background())
	if c.calls != 2 {
		t.Fatalf("calls next day = %d, want 2", c.calls)
	}
}

func TestCatchUp_NeverRunTaskRunsAndStateSurvivesNewRunner(t *testing.T) {
	now := time.Date(2026, 9, 29, 9, 0, 0, 0, time.Local)
	state := newMemoryState()

	c := &counter{}
	first := NewRunner(state, WithClock(fixedClock(&now)))
	first.Add(Task{Name: "purge", Run: c.run})
	first.CatchUp(context.Background())

	// A restart on the same day shares only the persisted State.
	second := NewRunner(state, WithClock(fixedClock(&now)))
	second.Add(Task{Name: "purge", Run: c.run})
	second.CatchUp(context.Background())

	if c.calls != 1 {
		t.Fatalf("calls = %d, want 1 (restart on the same day must not rerun)", c.calls)
	}
}

func TestCatchUp_FailureRetriesAfterDelayAndNotifiesOnceAtThreshold(t *testing.T) {
	now := time.Date(2026, 9, 29, 9, 0, 0, 0, time.Local)
	n := &fakeNotifier{}
	c := &counter{err: errors.New("drive not mounted")}
	r := NewRunner(nil, WithClock(fixedClock(&now)), WithNotifier(n), WithRetryDelay(30*time.Minute))
	r.Add(Task{Name: "backup", Label: "DBバックアップ", Run: c.run})

	r.CatchUp(context.Background())
	now = now.Add(10 * time.Minute)
	r.CatchUp(context.Background()) // still inside the retry delay
	if c.calls != 1 {
		t.Fatalf("calls inside retry delay = %d, want 1", c.calls)
	}

	for i := 0; i < 4; i++ {
		now = now.Add(31 * time.Minute)
		r.CatchUp(context.Background())
	}
	if c.calls != 5 {
		t.Fatalf("calls = %d, want 5", c.calls)
	}
	if len(n.calls) != 1 || n.calls[0] != FailureNotifyThreshold || n.tasks[0] != "DBバックアップ" {
		t.Fatalf("notifications = %v %v, want one at threshold %d", n.tasks, n.calls, FailureNotifyThreshold)
	}

	// Recovery resets the streak and the retry delay.
	c.err = nil
	now = now.Add(31 * time.Minute)
	r.CatchUp(context.Background())
	if c.calls != 6 {
		t.Fatalf("calls after recovery = %d, want 6", c.calls)
	}
}

func TestRunNow_RunsEvenAfterTodaysSuccess(t *testing.T) {
	now := time.Date(2026, 9, 29, 16, 0, 0, 0, time.Local)
	c := &counter{}
	r := NewRunner(nil, WithClock(fixedClock(&now)))
	r.Add(Task{Name: "backup", Run: c.run})

	r.CatchUp(context.Background())
	r.RunNow(context.Background(), "backup")
	r.RunNow(context.Background(), "unknown")
	if c.calls != 2 {
		t.Fatalf("calls = %d, want 2", c.calls)
	}
}

// A skipped task (not enabled in Settings yet) is neither a success nor a
// failure: it is retried on the very next CatchUp, never notifies, and
// succeeds as soon as it is enabled.
func TestCatchUp_SkippedTaskIsRetriedWithoutFailureAccounting(t *testing.T) {
	now := time.Date(2026, 10, 8, 9, 0, 0, 0, time.Local)
	state := newMemoryState()
	notifier := &fakeNotifier{}
	c := &counter{err: ErrSkipped}
	r := NewRunner(state, WithClock(fixedClock(&now)), WithNotifier(notifier))
	r.Add(Task{Name: "backup", Label: "backup", Run: c.run})

	for i := 0; i < FailureNotifyThreshold+1; i++ {
		r.CatchUp(context.Background())
	}
	if c.calls != FailureNotifyThreshold+1 {
		t.Fatalf("calls = %d, want %d (no retry delay for a skipped task)", c.calls, FailureNotifyThreshold+1)
	}
	if len(notifier.calls) != 0 {
		t.Fatalf("notifier called %v for a skipped task, want never", notifier.calls)
	}
	if _, ok, _ := state.Get(context.Background(), "system.maintenance.backup.last_success_date"); ok {
		t.Fatal("a skipped task recorded a success date")
	}

	c.err = nil // the operator configured the backup directory
	r.CatchUp(context.Background())
	if got, _, _ := state.Get(context.Background(), "system.maintenance.backup.last_success_date"); got != `"2026-10-08"` {
		t.Fatalf("persisted date after enabling = %s, want \"2026-10-08\"", got)
	}
}

// A task that returns ErrMore does its backlog in chunks. Between two chunks the
// runner's lock is free: a fixed-time trigger (RunNow of the backup) and another
// CatchUp get in, so a long job never keeps the backup or the retention purge
// waiting for more than one chunk (the research labels' first 90-day backfill).
// The hook runs in the gap and calls the runner itself: with the lock still held
// that would block forever, which the test turns into a failure by a deadline.
func TestCatchUp_ErrMoreReleasesTheLockBetweenChunks(t *testing.T) {
	now := time.Date(2026, 10, 14, 16, 5, 0, 0, time.Local)
	state := newMemoryState()
	chunks, backups, gaps := 0, 0, 0
	var r *Runner
	r = NewRunner(state, WithClock(fixedClock(&now)), WithChunkWait(func(ctx context.Context) {
		gaps++
		// A trigger at the fixed time and a periodic CatchUp, while the backfill is mid-way.
		r.RunNow(ctx, "backup")
		r.CatchUp(ctx)
	}))
	r.Add(Task{Name: "backup", Run: func(context.Context) error { backups++; return nil }})
	r.Add(Task{Name: "research", Run: func(context.Context) error {
		chunks++
		if chunks < 4 {
			return ErrMore
		}
		return nil
	}})

	done := make(chan struct{})
	go func() { r.CatchUp(context.Background()); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("CatchUp did not finish: the runner's lock was held between chunks")
	}
	if chunks != 4 {
		t.Errorf("chunks = %d, want 4 (a second CatchUp in a gap must not start the task over)", chunks)
	}
	if gaps != 3 {
		t.Errorf("gaps = %d, want 3 (one after each ErrMore)", gaps)
	}
	// backup: once by the first CatchUp loop, then in each gap by RunNow and by
	// the nested CatchUp (which skips it: it already succeeded today).
	if backups != 1+3 {
		t.Errorf("backup ran %d times, want 4 (1 catch-up + 3 fixed-time triggers inside the gaps)", backups)
	}
	if day, ok := r.lastSuccess(context.Background(), "research"); !ok || day != "2026-10-14" {
		t.Errorf("research last success = %q, %v; want today, recorded once the backlog is done", day, ok)
	}
}

// ErrMore is progress, not the day's success: until the last chunk no last-success
// date is stored, so a restart in the middle continues the backlog.
func TestCatchUp_ErrMoreIsNotASuccessUntilTheBacklogIsDone(t *testing.T) {
	now := time.Date(2026, 10, 14, 16, 5, 0, 0, time.Local)
	state := newMemoryState()
	ctx := context.Background()
	chunks := 0
	var r *Runner
	r = NewRunner(state, WithClock(fixedClock(&now)), WithChunkWait(func(ctx context.Context) {
		if _, ok := r.lastSuccess(ctx, "research"); ok {
			t.Error("a last-success date was recorded before the last chunk")
		}
	}))
	r.Add(Task{Name: "research", Run: func(context.Context) error {
		chunks++
		if chunks < 3 {
			return ErrMore
		}
		return nil
	}})
	r.CatchUp(ctx)
	if _, ok := r.lastSuccess(ctx, "research"); !ok || chunks != 3 {
		t.Fatalf("chunks = %d, success recorded = %v; want 3 chunks then a success", chunks, ok)
	}
}

// A failure in a later chunk is a failure like any other: counted, retried after
// the delay, and the days done by the earlier chunks stay done (the task's job).
func TestCatchUp_AFailureAfterErrMoreIsCountedAndRetriedLater(t *testing.T) {
	now := time.Date(2026, 10, 14, 16, 5, 0, 0, time.Local)
	calls := 0
	r := NewRunner(newMemoryState(), WithClock(fixedClock(&now)), WithRetryDelay(30*time.Minute), WithChunkWait(func(context.Context) {}))
	r.Add(Task{Name: "research", Run: func(context.Context) error {
		calls++
		if calls == 1 {
			return ErrMore
		}
		return errors.New("disk full")
	}})
	r.CatchUp(context.Background())
	if calls != 2 {
		t.Fatalf("calls = %d, want 2 (one chunk, then the failing one; no more chunks after a failure)", calls)
	}
	r.CatchUp(context.Background()) // inside the retry delay
	if calls != 2 {
		t.Fatalf("calls = %d, want the retry delay to hold", calls)
	}
	if _, ok := r.lastSuccess(context.Background(), "research"); ok {
		t.Fatal("a failed backlog was recorded as a success")
	}
}
