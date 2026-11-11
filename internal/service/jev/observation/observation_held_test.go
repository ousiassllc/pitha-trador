package observation_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/repository/jobqueue"
	"github.com/ousiassllc/pitha-trador/internal/service/jev"
)

// fakeHeld is the held-symbols source of the gate: the symbols with an open
// position or a pending order, or a read error.
type fakeHeld struct {
	symbols []string
	err     error
	reads   int
}

func (f *fakeHeld) HeldSymbols(context.Context) ([]string, error) {
	f.reads++
	return f.symbols, f.err
}

// A symbol that is listed for observation but held (an open position or a
// pending order) is managed as always: Scout asks Jev, and a passing answer goes
// on to Trader, so the position keeps its normal Trader and exit path. It is not
// held to the observation cap, and its Scout decision is indexed like any other.
func TestScout_AHeldObservationSymbolGoesThroughTheNormalTraderPath(t *testing.T) {
	r := newRig(t, true, "7203", "6758")
	r.observe(t, "7203, 6758")
	r.held.symbols = []string{"7203"}
	r.set(t, "jev.observation.daily_cost_cap_usd", "0.0000001")

	// The observation-only one still stops at Scout.
	if err := r.handle(t, "6758"); err != nil {
		t.Fatalf("HandleJob(observation symbol) = %v", err)
	}
	if got := r.traderJobs(t); len(got) != 0 {
		t.Fatalf("jev-trader jobs = %v for an observation-only symbol, want none", got)
	}

	// The held one is an ordinary symbol, also with the observation cap spent.
	for range 2 {
		if err := r.handle(t, "7203"); err != nil {
			t.Fatalf("HandleJob(held observation symbol) = %v, want the normal flow even over the observation cap", err)
		}
	}
	if got := r.traderJobs(t); len(got) != 2 || got[0] != "7203" || got[1] != "7203" {
		t.Errorf("jev-trader jobs = %v, want the held symbol's two normal Trader jobs", got)
	}
	var vectors int
	if err := r.db.QueryRow(`SELECT COUNT(*) FROM jev_decision_vectors`).Scan(&vectors); err != nil || vectors != 2 {
		t.Errorf("jev_decision_vectors = %d, %v; want the held symbol's two decisions indexed like ordinary ones", vectors, err)
	}

	// The position is closed: it is observation-only again.
	r.held.symbols = nil
	var capped *jobqueue.SkippedError
	if err := r.handle(t, "7203"); !errors.As(err, &capped) || capped.Reason != jev.ObservationCapSkip {
		t.Fatalf("HandleJob after the position closed = %v, want the observation cap skip: it is observation-only again", err)
	}
	if got := r.traderJobs(t); len(got) != 0 {
		t.Errorf("jev-trader jobs = %v after the position closed, want none", got)
	}
}

// Only a listed symbol costs a read of the held symbols: the ordinary path
// is not touched by the held lookup at all.
func TestGate_ReadsTheHeldSymbolsOnlyForAListedSymbol(t *testing.T) {
	r := newRig(t, true, "7203", "6758")
	ctx := context.Background()
	r.observe(t, "7203")
	for _, sym := range []string{"6758", "9984"} {
		if ok, err := r.gate.Observed(ctx, sym); err != nil || ok {
			t.Fatalf("Observed(%s) = %v, %v; want false", sym, ok, err)
		}
	}
	if r.held.reads != 0 {
		t.Fatalf("held reads = %d for symbols that are not listed, want 0", r.held.reads)
	}
	if ok, err := r.gate.Observed(ctx, "7203"); err != nil || !ok || r.held.reads != 1 {
		t.Fatalf("Observed(7203) = %v, %v after %d reads; want true after one", ok, err, r.held.reads)
	}
}

// When the standing of a symbol cannot be read, it is not dropped to "not
// observed" (which could send an observation-only symbol to Trader) and not
// failed into a retry loop: that round is skipped, with no Jev call, no entry
// and a log line, and the next job reads the standing again.
func TestScout_AnUnreadableObservationStandingSkipsTheSymbolForThatRoundOnly(t *testing.T) {
	r := newRig(t, true, "7203", "6758")
	r.observe(t, "7203")
	r.held.err = errors.New("database is locked")

	var skipped *jobqueue.SkippedError
	if err := r.handle(t, "7203"); !errors.As(err, &skipped) || skipped.Reason != jev.ObservationUnknownSkip {
		t.Fatalf("HandleJob with an unreadable standing = %v, want the skip", err)
	}
	if got := r.calls.Load(); got != 0 {
		t.Errorf("Jev calls = %d, want none for the skipped round", got)
	}
	if got := r.traderJobs(t); len(got) != 0 {
		t.Errorf("jev-trader jobs = %v, want no entry from the skipped round", got)
	}
	// Another symbol is unaffected, in the same round.
	if err := r.handle(t, "6758"); err != nil {
		t.Fatalf("HandleJob(another symbol) = %v", err)
	}
	if got := r.traderJobs(t); len(got) != 1 || got[0] != "6758" {
		t.Errorf("jev-trader jobs = %v, want the other symbol's", got)
	}

	// The next round reads again and goes on as an observation-only symbol.
	r.held.err = nil
	if err := r.handle(t, "7203"); err != nil {
		t.Fatalf("HandleJob on the next round = %v", err)
	}
	if got, trader := r.calls.Load(), r.traderJobs(t); got != 2 || len(trader) != 0 {
		t.Errorf("Jev calls = %d, jev-trader jobs = %v; want the observation call made and no Trader job", got, trader)
	}
}
