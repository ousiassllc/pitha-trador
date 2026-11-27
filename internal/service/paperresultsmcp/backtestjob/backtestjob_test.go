package backtestjob_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/repository/backtestrun"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest"
	"github.com/ousiassllc/pitha-trador/internal/service/backtestlab"
	"github.com/ousiassllc/pitha-trador/internal/service/backtestlab/labtest"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/backtestjob"
)

// memStore is an in-memory Store: it records what the manager wrote.
type memStore struct {
	mu        sync.Mutex
	created   []backtestrun.NewRun
	outcomes  map[int64]backtestrun.Outcome
	started   []int64
	sweeps    []string
	sweepErr  error
	createErr error
	// finishPanic makes FinishBacktestRun panic (a bug in the store).
	finishPanic bool
}

func (s *memStore) CreateBacktestRun(_ context.Context, client string, req backtestrun.NewRun) (backtestrun.Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.createErr != nil {
		return backtestrun.Run{}, s.createErr
	}
	req.Client = client
	s.created = append(s.created, req)
	return backtestrun.Run{ID: int64(len(s.created)), Status: backtestrun.StatusQueued, Owner: req.Owner}, nil
}

func (s *memStore) StartBacktestRun(_ context.Context, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.started = append(s.started, id)
	return nil
}

func (s *memStore) FinishBacktestRun(_ context.Context, id int64, out backtestrun.Outcome) error {
	if s.finishPanic {
		panic("store bug")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.outcomes == nil {
		s.outcomes = map[int64]backtestrun.Outcome{}
	}
	s.outcomes[id] = out
	return nil
}

func (s *memStore) FailOrphanBacktestRuns(_ context.Context, owner string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sweepErr != nil {
		return 0, s.sweepErr
	}
	s.sweeps = append(s.sweeps, owner)
	return 0, nil
}

func job() backtestjob.Job {
	req, err := backtestlab.Normalize(nil, nil)
	if err != nil {
		panic(err)
	}
	return backtestjob.Job{FromDate: "2026-10-05", ToDate: "2026-10-07", Period: labtest.Period(), ReplayMode: "paper", Request: req}
}

func newManager(store *memStore, source backtestjob.SourceFactory) (*backtestjob.Manager, chan int64) {
	done := make(chan int64, 8)
	m := backtestjob.New(backtestjob.Config{
		Source: source, Owner: "me", Store: func() (backtestjob.Store, error) { return store, nil },
		OnFinish: func(id int64) { done <- id },
	})
	return m, done
}

func TestStartWithoutASourceIsUnavailable(t *testing.T) {
	store := &memStore{}
	m, _ := newManager(store, nil)
	if _, err := m.Start(t.Context(), job()); !errors.Is(err, backtestjob.ErrUnavailable) {
		t.Fatalf("Start = %v, want ErrUnavailable", err)
	}
	if len(store.created) != 0 || len(store.sweeps) != 0 {
		t.Fatalf("something was written: %+v", store)
	}
}

func TestOrphansAreSweptOncePerProcessAndRetriedAfterAFailure(t *testing.T) {
	store := &memStore{sweepErr: errors.New("db locked")}
	m, done := newManager(store, func(context.Context, string) (backtestlab.ConfigSource, error) { return &labtest.Source{}, nil })
	if _, err := m.Start(t.Context(), job()); err == nil {
		t.Fatal("a failed sweep must fail the start (nothing was created)")
	}
	if len(store.created) != 0 {
		t.Fatalf("a run was created after a failed sweep")
	}
	store.mu.Lock()
	store.sweepErr = nil
	store.mu.Unlock()
	for range 2 {
		run, err := m.Start(t.Context(), job())
		if err != nil {
			t.Fatal(err)
		}
		if got := <-done; got != run.ID {
			t.Fatalf("finished %d, want %d", got, run.ID)
		}
	}
	if len(store.sweeps) != 1 || store.sweeps[0] != "me" {
		t.Fatalf("sweeps = %v, want exactly one, for this process's owner", store.sweeps)
	}
}

func TestACreateFailureFreesTheSlot(t *testing.T) {
	store := &memStore{createErr: errors.New("write disabled")}
	m, done := newManager(store, func(context.Context, string) (backtestlab.ConfigSource, error) { return &labtest.Source{}, nil })
	if _, err := m.Start(t.Context(), job()); err == nil {
		t.Fatal("want the create error")
	}
	store.mu.Lock()
	store.createErr = nil
	store.mu.Unlock()
	run, err := m.Start(t.Context(), job())
	if err != nil {
		t.Fatalf("the slot stayed taken after a refused create: %v", err)
	}
	<-done
	if out := store.outcomes[run.ID]; out.ErrorCode != "" || out.ResultJSON == "" {
		t.Fatalf("outcome = %+v", out)
	}
}

type panicSource struct{}

func (panicSource) RunConfigs(context.Context, backtest.Period) ([]backtest.RunConfig, error) {
	panic("engine bug")
}

// An engine panic is a failed run (internal), not a dead MCP, and frees the slot.
func TestAPanicInTheReplayIsAnInternalFailure(t *testing.T) {
	store := &memStore{}
	m, done := newManager(store, func(context.Context, string) (backtestlab.ConfigSource, error) { return panicSource{}, nil })
	run, err := m.Start(t.Context(), job())
	if err != nil {
		t.Fatal(err)
	}
	<-done
	if got := store.outcomes[run.ID].ErrorCode; got != backtestrun.ErrorInternal {
		t.Fatalf("error code %q, want internal", got)
	}
	if _, err := m.Start(t.Context(), job()); err != nil {
		t.Fatalf("the slot stayed taken after a panic: %v", err)
	}
	<-done
}

// The panic is not lost (FR-SCHED-6): its value and stack reach the log.
func TestAPanicInTheReplayIsLoggedWithItsStack(t *testing.T) {
	var logs bytes.Buffer
	store := &memStore{}
	done := make(chan int64, 1)
	m := backtestjob.New(backtestjob.Config{
		Source: func(context.Context, string) (backtestlab.ConfigSource, error) { return panicSource{}, nil },
		Owner:  "me", Store: func() (backtestjob.Store, error) { return store, nil },
		OnFinish: func(id int64) { done <- id },
		Logger:   slog.New(slog.NewTextHandler(&logs, nil)),
	})
	if _, err := m.Start(t.Context(), job()); err != nil {
		t.Fatal(err)
	}
	<-done
	// The channel send orders the replay's log write before this read.
	out := logs.String()
	for _, want := range []string{"backtest replay panicked", "panic=\"engine bug\"", "stack=", "RunConfigs"} {
		if !strings.Contains(out, want) {
			t.Errorf("log lacks %q:\n%s", want, out)
		}
	}
}

// A panic of the OnFinish hook (FR-SCHED-6) is logged, not a crash: Close still
// returns and the next start finds the slot free.
func TestAPanicInOnFinishDoesNotCrashOrHoldTheSlot(t *testing.T) {
	store := &memStore{}
	finished := make(chan struct{}, 2)
	m := backtestjob.New(backtestjob.Config{
		Source: func(context.Context, string) (backtestlab.ConfigSource, error) { return &labtest.Source{}, nil },
		Owner:  "me", Store: func() (backtestjob.Store, error) { return store, nil },
		OnFinish: func(int64) { finished <- struct{}{}; panic("hook bug") },
	})
	for range 2 {
		if _, err := m.Start(t.Context(), job()); err != nil {
			t.Fatalf("Start = %v (the slot stayed taken after an OnFinish panic?)", err)
		}
		<-finished
	}
	m.Close() // must not hang on wg.Wait
}

// A panic while the final status is written (FR-SCHED-6) frees the slot and the
// wait group: the next start is not busy and Close returns.
func TestAPanicInTheFinalWriteFreesTheSlot(t *testing.T) {
	store := &memStore{finishPanic: true}
	m, done := newManager(store, func(context.Context, string) (backtestlab.ConfigSource, error) { return &labtest.Source{}, nil })
	for range 2 {
		if _, err := m.Start(t.Context(), job()); err != nil {
			t.Fatalf("Start = %v (the slot stayed taken after a write panic?)", err)
		}
		<-done
	}
	m.Close() // must not hang on wg.Wait
}

func TestStartIsRefusedAfterClose(t *testing.T) {
	m, _ := newManager(&memStore{}, func(context.Context, string) (backtestlab.ConfigSource, error) { return &labtest.Source{}, nil })
	m.Close()
	if _, err := m.Start(t.Context(), job()); !errors.Is(err, backtestjob.ErrUnavailable) {
		t.Fatalf("Start after Close = %v, want ErrUnavailable", err)
	}
}

func TestTheRowCarriesTheRequestAndTheOwner(t *testing.T) {
	store := &memStore{}
	m, done := newManager(store, func(context.Context, string) (backtestlab.ConfigSource, error) { return &labtest.Source{}, nil })
	j := job()
	j.Client = "100.64.0.9"
	if _, err := m.Start(t.Context(), j); err != nil {
		t.Fatal(err)
	}
	<-done
	got := store.created[0]
	if got.Owner != "me" || got.Client != "100.64.0.9" || got.ExploredCount != 1 || got.ReplayMode != "paper" || got.FromDate != "2026-10-05" || got.CodeVersion == "" {
		t.Fatalf("created %+v", got)
	}
	if got.RequestJSON != `{"variants":[{"label":"baseline","params":{}}],"extra_cost_bps":[0,2,5,10]}` {
		t.Fatalf("request_json = %s", got.RequestJSON)
	}
}
