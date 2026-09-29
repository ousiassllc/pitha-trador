package supervisor

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"reflect"
	"testing"
	"time"
)

// fakeClock advances only when after() is called, recording each requested
// delay so tests can assert the backoff schedule without sleeping.
type fakeClock struct {
	t      time.Time
	delays []time.Duration
}

func (f *fakeClock) now() time.Time { return f.t }

func (f *fakeClock) after(d time.Duration) <-chan time.Time {
	f.delays = append(f.delays, d)
	f.t = f.t.Add(d)
	ch := make(chan time.Time, 1)
	ch <- f.t
	return ch
}

func testConfig(f *fakeClock) Config {
	return Config{MinBackoff: time.Second, MaxBackoff: 4 * time.Second, StableAfter: time.Minute, now: f.now, after: f.after}
}

func TestRun_CleanExitEndsSupervision(t *testing.T) {
	f := &fakeClock{}
	calls := 0
	err := Run(context.Background(), testConfig(f), func(context.Context) error {
		calls++
		return nil
	})
	if err != nil || calls != 1 || len(f.delays) != 0 {
		t.Fatalf("err=%v calls=%d delays=%v, want nil/1/none", err, calls, f.delays)
	}
}

func TestRun_CrashRestartsWithCappedBackoff(t *testing.T) {
	f := &fakeClock{}
	calls := 0
	err := Run(context.Background(), testConfig(f), func(context.Context) error {
		calls++
		if calls <= 5 {
			return errors.New("exit status 1")
		}
		return nil
	})
	if err != nil || calls != 6 {
		t.Fatalf("err=%v calls=%d, want nil/6", err, calls)
	}
	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 4 * time.Second, 4 * time.Second}
	if !reflect.DeepEqual(f.delays, want) {
		t.Fatalf("delays = %v, want %v", f.delays, want)
	}
}

func TestRun_StableRunResetsBackoff(t *testing.T) {
	f := &fakeClock{}
	calls := 0
	err := Run(context.Background(), testConfig(f), func(context.Context) error {
		calls++
		switch calls {
		case 1, 2:
			return errors.New("crash") // quick crashes grow the backoff
		case 3:
			f.t = f.t.Add(2 * time.Minute) // healthy run, then crash
			return errors.New("crash")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []time.Duration{time.Second, 2 * time.Second, time.Second}
	if !reflect.DeepEqual(f.delays, want) {
		t.Fatalf("delays = %v, want %v", f.delays, want)
	}
}

func TestRun_ContextCancelStopsRestarting(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	err := Run(ctx, Config{MinBackoff: time.Hour}, func(context.Context) error {
		calls++
		cancel()
		return errors.New("killed")
	})
	if !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatalf("err=%v calls=%d, want context.Canceled/1", err, calls)
	}
}

func TestRun_CancelDuringBackoffReturnsPromptly(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, Config{MinBackoff: time.Hour}, func(context.Context) error { return errors.New("crash") })
	}()
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after cancel during backoff")
	}
}

func TestChildArgs(t *testing.T) {
	got, found := ChildArgs([]string{"--supervise", "-x", "y"})
	if !found || !reflect.DeepEqual(got, []string{"-x", "y"}) {
		t.Fatalf("got %v %v", got, found)
	}
	if got, found := ChildArgs([]string{"-x"}); found || !reflect.DeepEqual(got, []string{"-x"}) {
		t.Fatalf("got %v %v", got, found)
	}
}

// TestHelperProcess is re-executed by TestExecRun as the supervised child;
// SUPERVISOR_TEST_EXIT selects its exit code.
func TestHelperProcess(t *testing.T) {
	code := os.Getenv("SUPERVISOR_TEST_EXIT")
	if code == "" {
		t.Skip("helper process only")
	}
	if code == "0" {
		os.Exit(0)
	}
	os.Exit(3)
}

func TestExecRun_ExitCodeMapping(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	run := ExecRun(exe, "-test.run=^TestHelperProcess$")

	t.Setenv("SUPERVISOR_TEST_EXIT", "0")
	if err := run(context.Background()); err != nil {
		t.Fatalf("exit 0: err = %v, want nil", err)
	}
	t.Setenv("SUPERVISOR_TEST_EXIT", "3")
	var exitErr *exec.ExitError
	if err := run(context.Background()); !errors.As(err, &exitErr) || exitErr.ExitCode() != 3 {
		t.Fatalf("exit 3: err = %v, want ExitError code 3", err)
	}
	if err := ExecRun("/nonexistent/pitha-child")(context.Background()); err == nil {
		t.Fatal("unstartable child: err = nil, want error (retried by Run)")
	}
}

func TestRun_RestartsRealChildUntilCleanExit(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("SUPERVISOR_TEST_EXIT", "3")
	crashes := 0
	inner := ExecRun(exe, "-test.run=^TestHelperProcess$")
	err = Run(context.Background(), Config{MinBackoff: time.Millisecond, MaxBackoff: time.Millisecond}, func(ctx context.Context) error {
		if crashes == 2 {
			t.Setenv("SUPERVISOR_TEST_EXIT", "0")
		}
		err := inner(ctx)
		if err != nil {
			crashes++
		}
		return err
	})
	if err != nil || crashes != 2 {
		t.Fatalf("err=%v crashes=%d, want nil/2", err, crashes)
	}
}
