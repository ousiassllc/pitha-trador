package safego_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/safego"
)

func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func TestRunLogsPanicWithStackAndReportsIt(t *testing.T) {
	buf := captureLog(t)
	if !safego.Run("unit", func() { panic("boom") }) {
		t.Fatal("Run should report the panic")
	}
	out := buf.String()
	for _, want := range []string{"unit", "boom", "stack", "safego_test"} {
		if !strings.Contains(out, want) {
			t.Errorf("log missing %q: %s", want, out)
		}
	}
	if safego.Run("ok", func() {}) {
		t.Error("Run reported a panic for a clean fn")
	}
}

func TestTryConvertsPanicToError(t *testing.T) {
	captureLog(t)
	err := safego.Try("unit", func() error { panic("boom") })
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err = %v, want panic error", err)
	}
	if err := safego.Try("unit", func() error { return nil }); err != nil {
		t.Fatalf("clean fn: %v", err)
	}
}

func TestRecoverKeepsProcessAlive(t *testing.T) {
	buf := captureLog(t)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer safego.Recover("goroutine")
		panic("boom")
	}()
	wg.Wait()
	if !strings.Contains(buf.String(), "boom") {
		t.Errorf("panic not logged: %s", buf.String())
	}
}

func TestLoopSurvivesPanickingAndFailingCycles(t *testing.T) {
	buf := captureLog(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	done := make(chan struct{})
	go func() {
		defer close(done)
		safego.Loop(ctx, "unit loop", func() time.Duration { return time.Millisecond }, func(context.Context) error {
			calls++
			switch calls {
			case 1:
				panic("boom")
			case 2:
				return errors.New("plain failure")
			case 4:
				cancel()
			}
			return nil
		})
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Loop did not exit")
	}
	if calls < 4 {
		t.Fatalf("cycles = %d, want the loop to continue past a panic and an error", calls)
	}
	out := buf.String()
	for _, want := range []string{"boom", "stack", "plain failure"} {
		if !strings.Contains(out, want) {
			t.Errorf("log missing %q: %s", want, out)
		}
	}
}
