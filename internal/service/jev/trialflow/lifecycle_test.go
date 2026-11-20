package trialflow_test

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/jevtrialsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain/jevtrial"
	"github.com/ousiassllc/pitha-trador/internal/service/jev"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/trial"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/trialwire"
	"github.com/ousiassllc/pitha-trador/internal/service/rag"
)

func TestCloseCancelsInFlightCallsAndLaterSubmitsAreDropped(t *testing.T) {
	e := newEnv(t, metalabelsettings.ModePaper, nil)
	started := make(chan struct{}, 4)
	a := e.addArm("a1", func(ctx context.Context, _ jev.TraderRequest) (trialwire.Response, time.Duration, int, error) {
		started <- struct{}{}
		<-ctx.Done()
		return trialwire.Response{}, 0, 3000, ctx.Err()
	})
	e.enable(defaultArm("a1"))
	live, err := e.trader.Evaluate(context.Background(), e.instID, state(e.clock()))
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("the arm call never started")
	}
	e.trial.Close()
	if n := e.count(`SELECT COUNT(*) FROM jev_trial_decisions`); n != 0 {
		t.Errorf("a call cancelled by Close stored %d rows, want 0", n)
	}
	if _, ok := e.get(jevtrialsettings.FailuresKey("a1")); ok {
		t.Error("a call cancelled by Close was counted as a failure")
	}
	e.trial.Submit(live, state(e.clock()), rag.Context{})
	e.trial.Wait()
	if a.calls() != 1 {
		t.Errorf("calls = %d, want 1 (a Submit after Close is dropped)", a.calls())
	}
	e.trial.Close() // idempotent
}

// attemptCountingResults counts how often the trial tried to store a result or
// a skip, whether or not the write succeeded. A slow or failed SQLite write
// then cannot change what the test observes about the queue.
type attemptCountingResults struct {
	trial.Results
	inserts, skips *atomic.Int32
}

func (r attemptCountingResults) Insert(ctx context.Context, d jevtrial.Decision) (jevtrial.Decision, error) {
	r.inserts.Add(1)
	return r.Results.Insert(ctx, d)
}

func (r attemptCountingResults) InsertSkip(ctx context.Context, s jevtrial.Skip) error {
	r.skips.Add(1)
	return r.Results.InsertSkip(ctx, s)
}

func TestQueueIsBoundedAtFourInFlight(t *testing.T) {
	var inserts, skips atomic.Int32
	e := newEnv(t, metalabelsettings.ModePaper, func(r trial.Results) trial.Results {
		return attemptCountingResults{Results: r, inserts: &inserts, skips: &skips}
	})
	started := make(chan struct{}, 8)
	release := make(chan struct{})
	var arms []jevtrialsettings.Arm
	var fakes []*fakeArm
	for i := range 6 {
		id := fmt.Sprintf("a%d", i)
		fakes = append(fakes, e.addArm(id, func(context.Context, jev.TraderRequest) (trialwire.Response, time.Duration, int, error) {
			started <- struct{}{}
			<-release
			return okResp(), time.Millisecond, 3000, nil
		}))
		arms = append(arms, defaultArm(id))
	}
	e.enable(arms...)
	if _, err := e.trader.Evaluate(context.Background(), e.instID, state(e.clock())); err != nil {
		t.Fatal(err)
	}
	for range 4 {
		select {
		case <-started:
		case <-time.After(10 * time.Second):
			t.Fatal("fewer than 4 arms started")
		}
	}
	// The dispatcher goroutine takes a slot per arm in order and records a skip
	// for each arm that finds all four taken. Release only once both skips are
	// recorded: releasing earlier frees slots while a4/a5 are still being
	// dispatched, and they would then take those slots and run. The skips and
	// results are counted where the trial tries to store them, not read back
	// from SQLite: a write that times out must not change what this test sees.
	waitFor(t, "the two comparisons without a slot to be recorded", func() bool {
		return skips.Load() == 2
	})
	close(release)
	e.trial.Wait()
	if n := inserts.Load(); n != 4 {
		t.Fatalf("results stored = %d, want 4 (the other 2 comparisons are dropped, not queued)", n)
	}
	if n := skips.Load(); n != 2 {
		t.Fatalf("skips recorded = %d, want 2", n)
	}
	total := 0
	for _, a := range fakes {
		total += a.calls()
	}
	if total != 4 {
		t.Fatalf("arm calls = %d, want 4", total)
	}
	if n := len(started); n != 0 {
		t.Fatalf("%d more arm calls started after the first 4, want 0", n)
	}
}
