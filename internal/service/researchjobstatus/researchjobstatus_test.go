package researchjobstatus_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/researchjobsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/researchjobstatus"
	"github.com/ousiassllc/pitha-trador/internal/service/scheduler/maintenance"
)

// store is an in-memory runtime_settings that counts its writes.
type store struct {
	mu     sync.Mutex
	rows   map[string]string
	writes int
}

func newStore() *store { return &store{rows: map[string]string{}} }

func (s *store) Get(_ context.Context, key string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.rows[key]
	return v, ok, nil
}

func (s *store) Set(_ context.Context, key, value string, _ time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rows[key] = value
	s.writes++
	return nil
}

var day = time.Date(2026, 11, 19, 17, 0, 0, 0, time.Local)

// runner is the real maintenance runner over st with the two tasks as nightlyjobs registers them
// (failure tracking on); outcome decides what both tasks return.
func runner(st *store, outcome *error) *maintenance.Runner {
	r := maintenance.NewRunner(st, maintenance.WithClock(func() time.Time { return day }))
	for _, name := range []string{researchjobstatus.EvalTask, researchjobstatus.ModelTask} {
		r.Add(maintenance.Task{Name: name, Label: name, TrackFailure: true, Run: func(context.Context) error { return *outcome }})
	}
	return r
}

func turnOn(t *testing.T, st *store, keys ...string) {
	t.Helper()
	for _, k := range keys {
		st.rows[k] = "1"
	}
}

func tasks(t *testing.T, st *store) map[string]researchjobstatus.Task {
	t.Helper()
	got, err := researchjobstatus.New(st).Tasks(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != researchjobstatus.EvalTask || got[1].Name != researchjobstatus.ModelTask {
		t.Fatalf("want the evaluation record then the model: %+v", got)
	}
	return map[string]researchjobstatus.Task{got[0].Name: got[0], got[1].Name: got[1]}
}

// Off (nothing stored, or switched off) is its own state - not a failure - and reading it writes nothing.
func TestTasks_OffIsNotAFailureAndWritesNothing(t *testing.T) {
	st := newStore()
	st.rows[researchjobsettings.KeyModelEnabled] = "0"
	// An old failure of a task that has since been switched off must not read as a failure.
	st.rows["system.maintenance.research_model.last_failure"] = `{"date":"2026-11-01","reason":"boom"}`
	before := st.writes
	for name, task := range tasks(t, st) {
		if task.State != researchjobstatus.Off || task.FailureReason != "" || task.FailureDate != "" {
			t.Errorf("%s: want off without a failure: %+v", name, task)
		}
	}
	if st.writes != before {
		t.Errorf("reading the status wrote %d rows", st.writes-before)
	}
}

// On with a success: the last success date is shown, and an earlier success stays visible once off.
func TestTasks_OnShowsLastSuccessDate(t *testing.T) {
	st := newStore()
	turnOn(t, st, researchjobsettings.KeyEvalEnabled, researchjobsettings.KeyModelEnabled)
	var outcome error
	r := runner(st, &outcome)
	// Before any run: on, but nothing succeeded or failed yet.
	if got := tasks(t, st)[researchjobstatus.EvalTask]; got.State != researchjobstatus.NotYetSucceeded || got.LastSuccessDate != "" {
		t.Fatalf("before the first run: %+v", got)
	}
	r.CatchUp(t.Context())
	for name, task := range tasks(t, st) {
		if task.State != researchjobstatus.Succeeded || task.LastSuccessDate != "2026-11-19" || task.FailureReason != "" {
			t.Errorf("%s: want succeeded on 2026-11-19: %+v", name, task)
		}
	}
	st.rows[researchjobsettings.KeyEvalEnabled] = "0"
	if got := tasks(t, st)[researchjobstatus.EvalTask]; got.State != researchjobstatus.Off || got.LastSuccessDate != "2026-11-19" {
		t.Errorf("switched off: want off with the earlier success kept: %+v", got)
	}
}

// On with a failure: the day and a short single-line reason are shown; a later success clears it.
func TestTasks_OnShowsFailureWithShortReasonAndRecovers(t *testing.T) {
	st := newStore()
	turnOn(t, st, researchjobsettings.KeyEvalEnabled)
	outcome := errors.New("nightlyjobs: record the Jev evaluation: " + strings.Repeat("x", 500) + "\nsecond line")
	r := runner(st, &outcome)
	r.CatchUp(t.Context())

	got := tasks(t, st)[researchjobstatus.EvalTask]
	if got.State != researchjobstatus.Failed || got.FailureDate != "2026-11-19" || got.LastSuccessDate != "" {
		t.Fatalf("want failed on 2026-11-19 without a success: %+v", got)
	}
	if strings.ContainsAny(got.FailureReason, "\n\r") || len([]rune(got.FailureReason)) > maintenance.MaxFailureReasonRunes+1 || !strings.HasPrefix(got.FailureReason, "nightlyjobs: record the Jev evaluation:") {
		t.Errorf("the reason must be one short line that starts with the error: %q", got.FailureReason)
	}
	// The other task is off: its state is off, not failed, although the runner failed it too.
	if other := tasks(t, st)[researchjobstatus.ModelTask]; other.State != researchjobstatus.Off {
		t.Errorf("model switched off: %+v", other)
	}

	outcome = nil
	day = day.Add(time.Hour) // the retry delay has passed
	defer func() { day = day.Add(-time.Hour) }()
	r.RunNow(t.Context(), researchjobstatus.EvalTask)
	if got := tasks(t, st)[researchjobstatus.EvalTask]; got.State != researchjobstatus.Succeeded || got.FailureReason != "" {
		t.Errorf("a later success must clear the failure: %+v", got)
	}
}

// A task without TrackFailure (every other maintenance task) leaves no failure row.
func TestRunner_OtherTasksStoreNoFailure(t *testing.T) {
	st := newStore()
	r := maintenance.NewRunner(st, maintenance.WithClock(func() time.Time { return day }))
	r.Add(maintenance.Task{Name: "backup", Label: "backup", Run: func(context.Context) error { return errors.New("disk full") }})
	r.CatchUp(t.Context())
	if st.writes != 0 {
		t.Errorf("a failing untracked task wrote %d rows: %v", st.writes, st.rows)
	}
}
