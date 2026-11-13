package trialflow_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/jevtrialsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/jev"
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

func TestQueueIsBoundedAtFourInFlight(t *testing.T) {
	e := newEnv(t, metalabelsettings.ModePaper, nil)
	started := make(chan struct{}, 8)
	release := make(chan struct{})
	var arms []jevtrialsettings.Arm
	for i := range 6 {
		id := fmt.Sprintf("a%d", i)
		e.addArm(id, func(context.Context, jev.TraderRequest) (trialwire.Response, time.Duration, int, error) {
			started <- struct{}{}
			<-release
			return okResp(), time.Millisecond, 3000, nil
		})
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
	close(release)
	e.trial.Wait()
	if n := e.count(`SELECT COUNT(*) FROM jev_trial_decisions`); n != 4 {
		t.Fatalf("rows = %d, want 4 (the other 2 comparisons are dropped, not queued)", n)
	}
}
