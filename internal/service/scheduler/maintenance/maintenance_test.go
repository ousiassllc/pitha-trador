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
