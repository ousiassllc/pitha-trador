package sleepguard_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
	"github.com/ousiassllc/pitha-trador/internal/service/sleepguard"
)

// at is a JST instant on 2026-10-<day>: the 20th is a Tuesday (trading day),
// the 24th a Saturday.
func at(day, hour, minute int) time.Time {
	return time.Date(2026, 10, day, hour, minute, 0, 0, marketcalendar.JST)
}

type fakeKeeper struct {
	held     bool
	holds    int
	releases int
	holdErr  error
	holdSeen chan struct{} // receives one value per Hold when non-nil
}

func (k *fakeKeeper) Hold() error {
	if k.holdErr != nil {
		return k.holdErr
	}
	k.held = true
	k.holds++
	if k.holdSeen != nil {
		k.holdSeen <- struct{}{}
	}
	return nil
}

func (k *fakeKeeper) Release() error {
	k.held = false
	k.releases++
	return nil
}

type fakeSettings struct {
	on  bool
	err error
}

func (s *fakeSettings) SleepGuardEnabled(context.Context) (bool, error) { return s.on, s.err }

type rig struct {
	guard    *sleepguard.Guard
	keeper   *fakeKeeper
	settings *fakeSettings
	now      time.Time
}

func newRig(on bool) *rig {
	r := &rig{keeper: &fakeKeeper{}, settings: &fakeSettings{on: on}, now: at(20, 8, 0)}
	r.guard = sleepguard.New(sleepguard.Config{Settings: r.settings, Keeper: r.keeper, Now: func() time.Time { return r.now }})
	return r
}

func (r *rig) check(t *testing.T, now time.Time) {
	t.Helper()
	r.now = now
	if err := r.guard.Check(context.Background()); err != nil {
		t.Fatalf("Check at %v: %v", now, err)
	}
}

// issue #863: with the switch on, the PC is held from 08:45 to the close of a
// trading day and given back after the close.
func TestSleepGuardOnlyDuringTradingHours(t *testing.T) {
	r := newRig(true)
	for _, c := range []struct {
		now  time.Time
		held bool
	}{
		{at(20, 8, 44), false}, {at(20, 8, 45), true}, {at(20, 12, 0), true}, // lunch break included
		{at(20, 15, 29), true}, {at(20, 15, 30), false}, {at(20, 22, 0), false},
	} {
		r.check(t, c.now)
		if r.keeper.held != c.held {
			t.Errorf("at %v held = %v, want %v", c.now, r.keeper.held, c.held)
		}
	}
	if r.keeper.holds != 1 || r.keeper.releases != 1 {
		t.Errorf("holds/releases = %d/%d, want 1/1 (idempotent across checks)", r.keeper.holds, r.keeper.releases)
	}
}

func TestSleepGuardDoesNothingWhenOff(t *testing.T) {
	r := newRig(false)
	for _, now := range []time.Time{at(20, 8, 45), at(20, 10, 0), at(20, 15, 30)} {
		r.check(t, now)
	}
	if r.keeper.holds != 0 || r.keeper.releases != 0 {
		t.Errorf("holds/releases = %d/%d, want nothing while the switch is off", r.keeper.holds, r.keeper.releases)
	}
}

func TestSleepGuardDoesNothingOnANonTradingDay(t *testing.T) {
	r := newRig(true)
	for _, now := range []time.Time{at(24, 10, 0), at(12, 10, 0)} { // Saturday, a holiday
		r.check(t, now)
	}
	if r.keeper.holds != 0 {
		t.Errorf("holds = %d, want none on a non-trading day", r.keeper.holds)
	}
}

// Turning the switch off during the window gives the PC back at the next
// check, and back on holds it again.
func TestSleepGuardFollowsTheSwitchWithoutRestart(t *testing.T) {
	r := newRig(true)
	r.check(t, at(20, 9, 0))
	r.settings.on = false
	r.check(t, at(20, 9, 1))
	if r.keeper.held {
		t.Fatal("still held after the switch went off")
	}
	r.settings.on = true
	r.check(t, at(20, 9, 2))
	if !r.keeper.held {
		t.Fatal("not held after the switch went on again")
	}
}

// The app exiting inside the window gives the PC back; closing twice, or when
// nothing is held, does nothing more.
func TestSleepGuardCloseGivesBackOnAppExit(t *testing.T) {
	r := newRig(true)
	r.check(t, at(20, 9, 0))
	if err := r.guard.Close(); err != nil || r.keeper.held {
		t.Fatalf("Close = %v, held = %v; want released", err, r.keeper.held)
	}
	if err := r.guard.Close(); err != nil || r.keeper.releases != 1 {
		t.Fatalf("second Close = %v, releases = %d; want a no-op", err, r.keeper.releases)
	}
}

// Run gives the PC back when its context ends (the app exits).
func TestSleepGuardRunReleasesWhenContextEnds(t *testing.T) {
	r := newRig(true)
	r.now = at(20, 9, 0)
	r.keeper.holdSeen = make(chan struct{}, 1)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { r.guard.Run(ctx); close(done) }()
	select {
	case <-r.keeper.holdSeen:
	case <-time.After(5 * time.Second):
		t.Fatal("the guard never held the PC")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
	if r.keeper.releases != 1 {
		t.Errorf("releases = %d, want 1 on exit", r.keeper.releases)
	}
}

func TestSleepGuardFailedHoldIsRetriedAtTheNextCheck(t *testing.T) {
	r := newRig(true)
	r.keeper.holdErr = errors.New("boom")
	r.now = at(20, 9, 0)
	if err := r.guard.Check(context.Background()); err == nil {
		t.Fatal("Check = nil, want the Hold error")
	}
	r.keeper.holdErr = nil
	r.check(t, at(20, 9, 1))
	if !r.keeper.held {
		t.Fatal("not held after the retry")
	}
}

func TestSleepGuardSettingsReadFailureKeepsTheState(t *testing.T) {
	r := newRig(true)
	r.check(t, at(20, 9, 0))
	r.settings.err = errors.New("db down")
	if err := r.guard.Check(context.Background()); err == nil {
		t.Fatal("Check = nil, want the settings error")
	}
	if !r.keeper.held {
		t.Fatal("a failed settings read must not release the PC")
	}
}
