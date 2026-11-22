package guard_test

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana/guard"
	tt "github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana/tachibanatest"
)

// supervised runs Supervise on a manual clock in a goroutine. fn is called
// once per run with the run number (1-based); it panics while panicsOn says so
// and may advance the clock to simulate a run that lived.
type supervised struct {
	clk     *tt.ManualClock
	runs    chan int
	gaveUp  chan bool
	restart chan struct{}
}

func start(ctx context.Context, panicsOn func(run int) bool, lived func(run int) time.Duration) *supervised {
	s := &supervised{
		clk:     tt.NewManualClock(tt.AtJST(2026, 10, 8, 10, 0)),
		runs:    make(chan int, 64),
		gaveUp:  make(chan bool, 1),
		restart: make(chan struct{}, 64),
	}
	n := 0
	go func() {
		s.gaveUp <- guard.Supervise(ctx, s.clk, "test", func() {
			n++
			s.runs <- n
			s.clk.Advance(lived(n))
			if panicsOn(n) {
				panic("boom")
			}
		}, func() { s.restart <- struct{}{} })
	}()
	return s
}

// nextRun waits until run number want has started.
func (s *supervised) nextRun(t *testing.T, want int) {
	t.Helper()
	select {
	case got := <-s.runs:
		if got != want {
			t.Fatalf("run %d, want %d", got, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("run %d did not start", want)
	}
}

func (s *supervised) waitTimer(t *testing.T, created int) {
	t.Helper()
	s.clk.WaitCreated(t, created)
}

func TestSuperviseBacksOffDoublingToTheCapThenGivesUp(t *testing.T) {
	s := start(context.Background(), func(int) bool { return true }, func(int) time.Duration { return 0 })
	want := []time.Duration{5 * time.Second, 10 * time.Second, 20 * time.Second, 40 * time.Second, time.Minute}
	if len(want) != guard.MaxRestarts || want[0] != guard.BackoffMin || want[len(want)-1] != guard.BackoffMax {
		t.Fatalf("the expected delays %v no longer match the constants", want)
	}
	s.nextRun(t, 1)
	for i, d := range want {
		s.waitTimer(t, i) // restart i+1's delay timer is the (i+1)th ever created
		s.clk.Advance(d - time.Nanosecond)
		select {
		case <-s.runs:
			t.Fatalf("restart %d came before %v", i+1, d)
		case <-time.After(20 * time.Millisecond):
		}
		s.clk.Advance(time.Nanosecond)
		s.nextRun(t, i+2)
	}
	select {
	case gaveUp := <-s.gaveUp:
		if !gaveUp {
			t.Error("Supervise did not report giving up")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Supervise did not return after the last panic")
	}
	if len(s.restart) != guard.MaxRestarts {
		t.Errorf("restart hooks = %d, want %d", len(s.restart), guard.MaxRestarts)
	}
}

// A run that lived HealthyAfter before panicking starts the count over, so a
// flaky-but-mostly-fine goroutine is not given up on.
func TestSuperviseCountsAgainAfterAHealthyRun(t *testing.T) {
	panicking := guard.MaxRestarts + 3 // more panics than the limit allows in a row
	s := start(context.Background(),
		func(run int) bool { return run <= panicking },
		func(run int) time.Duration {
			if run%2 == 0 {
				return guard.HealthyAfter // every other run was healthy
			}
			return 0
		})
	s.nextRun(t, 1)
	for run := 2; run <= panicking+1; run++ {
		s.waitTimer(t, run-2) // the delay after run-1's panic is armed
		s.clk.Advance(guard.BackoffMax)
		s.nextRun(t, run)
	}
	select {
	case gaveUp := <-s.gaveUp:
		if gaveUp {
			t.Error("gave up although healthy runs reset the count")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Supervise did not return after a clean run")
	}
}

func TestSuperviseReturnsWithoutRestartWhenContextEnds(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	s := start(ctx, func(int) bool { return true }, func(int) time.Duration { return 0 })
	s.nextRun(t, 1)
	s.waitTimer(t, 0)
	cancel()
	select {
	case gaveUp := <-s.gaveUp:
		if gaveUp {
			t.Error("a canceled supervisor must not report giving up")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Supervise did not return after ctx was canceled")
	}
	if len(s.restart) != 0 || len(s.runs) != 0 {
		t.Error("a canceled supervisor must not run or restart again")
	}
}
