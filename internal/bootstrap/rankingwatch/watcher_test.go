package rankingwatch_test

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"errors"

	"github.com/ousiassllc/pitha-trador/internal/service/broker"
)

func TestWatcher_RankingDrivesWatchListRegistrationAndIngestion(t *testing.T) {
	r := newRig(t, "7203", "6758", "9984")
	r.src.set([]string{"7203", "9999", "6758"}, nil, false) // 9999 is not in the universe

	r.cycle()

	want := []string{"6758", "7203"}
	if got := sorted(r.lastEnqueued()); !slices.Equal(got, want) {
		t.Errorf("enqueued = %v, want %v (ranked symbols in the universe only)", got, want)
	}
	if len(r.reg.sets) != 1 || !slices.Equal(sorted(r.reg.sets[0]), want) {
		t.Errorf("registered = %v, want %v once", r.reg.sets, want)
	}
	if !r.list.Contains("7203") || r.list.Contains("9984") || r.list.Contains("9999") {
		t.Errorf("watch list wrong: 7203=%v 9984=%v 9999=%v", r.list.Contains("7203"), r.list.Contains("9984"), r.list.Contains("9999"))
	}
	r.cycle() // unchanged list: not registered again
	if len(r.reg.sets) != 1 {
		t.Errorf("registrations = %d, want 1 for an unchanged list", len(r.reg.sets))
	}
}

func TestWatcher_EmptyRankingMeansZeroCandidatesAndKeepsHeldSlots(t *testing.T) {
	r := newRig(t, "7203", "6758")
	r.held.symbols = []string{"6758"}
	r.src.set([]string{"7203"}, nil, false)
	r.cycle()
	if !r.list.Contains("7203") || !r.list.Contains("6758") {
		t.Fatal("setup: ranked and held symbols should be watched")
	}

	r.src.set(nil, nil, false) // 平日 7:53〜9:00過ぎ
	r.cycle()

	if r.list.Contains("7203") {
		t.Error("stale ranked symbol still watched after an empty ranking")
	}
	if !r.list.Contains("6758") || r.list.Len() != 1 {
		t.Errorf("watch list = %d symbols, want only the held 6758", r.list.Len())
	}
	if got := r.lastEnqueued(); !slices.Equal(got, []string{"6758"}) {
		t.Errorf("enqueued = %v, want only the held symbol", got)
	}
	if last := r.reg.sets[len(r.reg.sets)-1]; !slices.Equal(last, []string{"6758"}) {
		t.Errorf("registered = %v, want only the held symbol", last)
	}
	if !strings.Contains(r.logs.String(), "ranking is empty") {
		t.Errorf("empty ranking not logged: %s", r.logs.String())
	}
}

func TestWatcher_EmptyRankingWithNothingHeldPublishesEmptyList(t *testing.T) {
	r := newRig(t, "7203")
	r.src.set([]string{"7203"}, nil, false)
	r.cycle()
	r.src.set(nil, nil, false)
	r.cycle()
	if r.list.Len() != 0 {
		t.Errorf("watch list = %d symbols, want 0", r.list.Len())
	}
	if last := r.reg.sets[len(r.reg.sets)-1]; len(last) != 0 {
		t.Errorf("registered = %v, want the registration emptied", last)
	}
}

func TestWatcher_HeldReadErrorKeepsPreviousHeldSymbols(t *testing.T) {
	r := newRig(t, "7203", "6758")
	r.held.symbols = []string{"6758"}
	r.src.set(nil, nil, false)
	r.cycle()
	r.held.symbols, r.held.err = nil, errors.New("db locked")
	r.cycle()
	if !r.list.Contains("6758") {
		t.Error("held slot dropped by a transient held-symbol read error")
	}
}

func TestWatcher_RegistrationFailureOrPanicIsRetriedNextCycle(t *testing.T) {
	r := newRig(t, "7203")
	r.src.set([]string{"7203"}, nil, false)
	r.reg.err = broker.ErrNoSession
	r.cycle()
	r.reg.err, r.reg.panics = nil, true
	r.cycle()
	r.reg.panics = false
	r.cycle()
	if len(r.reg.sets) != 1 || !slices.Equal(r.reg.sets[0], []string{"7203"}) {
		t.Fatalf("registrations = %v, want the list registered once it works again", r.reg.sets)
	}
	if len(r.ing.enqueued) != 3 {
		t.Errorf("ingestion ran %d times, want every cycle despite registration failures", len(r.ing.enqueued))
	}
}

func TestWatcher_RunCyclesUntilCancelled(t *testing.T) {
	r := newRig(t, "7203")
	r.src.set([]string{"7203"}, nil, false)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { r.w.Run(ctx, time.Millisecond); close(done) }()
	deadline := time.Now().Add(5 * time.Second)
	for !r.list.Contains("7203") && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	cancel()
	<-done
	if !r.list.Contains("7203") {
		t.Error("Run never published the watch list")
	}
}
