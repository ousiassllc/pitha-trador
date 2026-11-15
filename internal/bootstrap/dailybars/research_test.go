package dailybars_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap/dailybars"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/researchstatus"
	"github.com/ousiassllc/pitha-trador/internal/service/broker"
	tt "github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana/tachibanatest"
)

var night1 = tt.AtJST(2026, 10, 8, 18, 0) // the night of 2026-10-08

func (f *researchFixture) threeSymbols() {
	for _, s := range []string{"1001", "1002", "1003"} {
		f.addSymbol(s, bar(s, "2026-10-07", 100), bar(s, "2026-10-08", 101))
	}
}

// With the switch on, the history lands in daily_bars (kabu stays the broker:
// the collection only needs a source), the run and progress rows are written,
// and the nightly batch's own run table is untouched.
func TestResearchStoresTheHistoryAndRecordsTheProgress(t *testing.T) {
	f := newResearchFixture(t, night1)
	f.threeSymbols()
	f.source.errs["1002"] = errors.New("boom")
	f.runDue(t, f.newResearch())

	if got, want := f.stored(t, "1001", "1002", "1003"), []string{"1001/2026-10-07/100", "1001/2026-10-08/101", "1003/2026-10-07/100", "1003/2026-10-08/101"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("daily_bars = %v, want %v", got, want)
	}
	run := f.run(t, "2026-10-08")
	if run.Status != domain.DailyBarRunSucceeded || run.Symbols != 3 || run.Requests != 3 || run.SavedBars != 4 || run.Failed != 1 {
		t.Fatalf("run = %+v", run)
	}
	last, err := f.store.LastNights(context.Background())
	if err != nil || len(last) != 3 || last["1002"] != "2026-10-08" {
		t.Fatalf("progress nights = %v, %v; the failed symbol is attempted too", last, err)
	}
	if got := f.state(t); got != researchstatus.StateOK {
		t.Fatalf("status = %q, want ok", got)
	}
	if _, found, _ := f.nightly.Get(context.Background(), "2026-10-08"); found {
		t.Fatal("the research night wrote a daily_bar_runs row")
	}
	// A night that succeeded is not run again.
	calls := len(f.source.calls)
	f.runDue(t, f.newResearch())
	if len(f.source.calls) != calls {
		t.Fatalf("a succeeded night sent %d more requests", len(f.source.calls)-calls)
	}
}

// A later night stores only the new days; a symbol that failed the first
// night is backfilled by the next one.
func TestResearchLaterNightsStoreOnlyNewDaysAndBackfillFailures(t *testing.T) {
	f := newResearchFixture(t, night1)
	f.threeSymbols()
	f.source.errs["1002"] = errors.New("boom")
	r := f.newResearch()
	f.runDue(t, r)

	delete(f.source.errs, "1002")
	for _, s := range []string{"1001", "1002", "1003"} {
		f.source.history[s] = append(f.source.history[s], bar(s, "2026-10-09", 102))
	}
	f.clock.set(tt.AtJST(2026, 10, 9, 18, 0))
	f.runDue(t, r)

	if run := f.run(t, "2026-10-09"); run.SavedBars != 5 { // a new day each of 1001 and 1003, and the whole history (2 + 1) of 1002
		t.Fatalf("night 2 saved %d bars, want 5", run.SavedBars)
	}
	if got := len(f.stored(t, "1001", "1002", "1003")); got != 9 {
		t.Fatalf("stored bars = %d, want 9", got)
	}
	if row := f.progressRow(t, "1002"); row.first != "2026-10-09" || row.saved != 3 || row.failures != 1 {
		t.Fatalf("progress of 1002 = %+v, want first_ok 2026-10-09, 3 bars, 1 failure", row)
	}
}

// Switch off, unset credentials, 立花 as the broker, or not due yet: no
// request, and for the first two not even an Acquire.
func TestResearchSendsNothingWhenOffUnconfiguredOrNotDue(t *testing.T) {
	for name, tc := range map[string]struct {
		now      time.Time
		mutate   func(*researchFixture)
		state    string
		acquires int
	}{
		"switch off":          {night1, func(f *researchFixture) { f.settings.Research.BarsEnabled = false }, researchstatus.StateDisabled, 0},
		"before the run time": {tt.AtJST(2026, 10, 8, 17, 59), nil, researchstatus.StateWaiting, 0},
		"daytime":             {tt.AtJST(2026, 10, 8, 12, 0), nil, researchstatus.StateWaiting, 0},
		"closed window":       {tt.AtJST(2026, 10, 9, 4, 0), func(f *researchFixture) { f.settings.Nightly.RunTime = "01:00" }, researchstatus.StateWaiting, 0},
		"credentials unset": {night1, func(f *researchFixture) {
			f.acquireErr = fmt.Errorf("%w: wrapped", dailybars.ErrNotConfigured)
		}, researchstatus.StateNotConfigured, 1},
		"tachibana is the broker": {night1, func(f *researchFixture) { f.acquireErr = dailybars.ErrBrokerIsTachibana }, researchstatus.StateBrokerIsTachibana, 1},
	} {
		t.Run(name, func(t *testing.T) {
			f := newResearchFixture(t, tc.now)
			f.threeSymbols()
			if tc.mutate != nil {
				tc.mutate(f)
			}
			f.runDue(t, f.newResearch())
			if n := len(f.source.calls); n != 0 {
				t.Fatalf("%d requests sent, want 0", n)
			}
			if got := f.acquireCount(); got != tc.acquires {
				t.Fatalf("Acquire called %d times, want %d", got, tc.acquires)
			}
			if got := f.state(t); got != tc.state {
				t.Fatalf("status = %q, want %q", got, tc.state)
			}
			if _, found, _ := f.store.Get(context.Background(), "2026-10-08"); found {
				t.Fatal("a run row was written")
			}
		})
	}
}

// A night cut short (the session lost, or the process stopping) and picked up
// by a new instance does not request again the symbols it attempted, and
// ends with the data of an uninterrupted night.
func TestResearchInterruptedNightResumesWithoutRepeatingAttemptedSymbols(t *testing.T) {
	whole := newResearchFixture(t, night1)
	whole.threeSymbols()
	whole.runDue(t, whole.newResearch())
	want := whole.stored(t, "1001", "1002", "1003")

	for name, cut := range map[string]struct {
		interrupt func(f *researchFixture, cancel context.CancelFunc, symbol string)
	}{
		"session lost": {func(f *researchFixture, _ context.CancelFunc, s string) {
			if s == "1002" {
				f.source.errs["1002"] = broker.ErrNoSession
			}
		}},
		"stopped": {func(_ *researchFixture, cancel context.CancelFunc, s string) {
			if s == "1002" {
				cancel()
			}
		}},
	} {
		t.Run(name, func(t *testing.T) {
			f := newResearchFixture(t, night1)
			f.threeSymbols()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			f.source.before = func(s string) { cut.interrupt(f, cancel, s) }
			f.runDueCtx(t, ctx, f.newResearch())
			if got := f.run(t, "2026-10-08"); got.Status != domain.DailyBarRunFailed {
				t.Fatalf("interrupted run = %+v, want failed", got)
			}
			if got := f.state(t); got != researchstatus.StateError {
				t.Fatalf("status = %q, want error", got)
			}

			// A new instance (a restart), the cause gone, 11 minutes later.
			delete(f.source.errs, "1002")
			f.source.before = nil
			f.source.calls = nil
			f.clock.set(f.clock.Now().Add(11 * time.Minute))
			f.runDue(t, f.newResearch())

			// 1001 was attempted before the cut; the cut request is asked again.
			if wantCalls := []string{"1002", "1003"}; !reflect.DeepEqual(f.source.calls, wantCalls) {
				t.Fatalf("resumed requests = %v, want %v", f.source.calls, wantCalls)
			}
			if got := f.stored(t, "1001", "1002", "1003"); !reflect.DeepEqual(got, want) {
				t.Fatalf("daily_bars = %v, want the uninterrupted %v", got, want)
			}
			if run := f.run(t, "2026-10-08"); run.Status != domain.DailyBarRunSucceeded || run.Symbols != 3 {
				t.Fatalf("resumed run = %+v", run)
			}
		})
	}
}

// A night cut short by the 8:00 window continues the next night with the
// symbols it did not reach, then the others.
func TestResearchNextNightContinuesWhereTheShortNightStopped(t *testing.T) {
	f := newResearchFixture(t, tt.AtJST(2026, 10, 9, 7, 59, 58)) // two requests fit before 8:00
	f.settings.Nightly.RunTime = "01:00"
	for _, s := range []string{"1001", "1002", "1003", "1004"} {
		f.addSymbol(s, bar(s, "2026-10-08", 100))
	}
	r := f.newResearch()
	f.runDue(t, r)
	if fmt.Sprint(f.source.calls) != "[1001 1002]" {
		t.Fatalf("requests = %v, want to stop at 08:00", f.source.calls)
	}
	if run := f.run(t, "2026-10-08"); run.Status != domain.DailyBarRunFailed {
		t.Fatalf("run = %+v, want failed", run)
	}

	f.source.calls = nil
	f.settings.Nightly.RunTime = "18:00"
	f.clock.set(tt.AtJST(2026, 10, 9, 18, 0)) // the next night: the 9th
	f.runDue(t, r)
	if fmt.Sprint(f.source.calls) != "[1003 1004 1001 1002]" {
		t.Fatalf("next night requests = %v, want the unreached symbols first", f.source.calls)
	}
}

// The configured rate spaces the requests, and none leaves in 8:00〜15:30.
func TestResearchKeepsTheRateAndNeverRequestsInTheDaytime(t *testing.T) {
	f := newResearchFixture(t, night1)
	f.settings.Nightly.MaxPerSecond = 0.5
	f.threeSymbols()
	f.runDue(t, f.newResearch())
	for i := 1; i < len(f.source.times); i++ {
		if gap := f.source.times[i].Sub(f.source.times[i-1]); gap < 2*time.Second {
			t.Errorf("gap before request %d = %s, want >= 2s", i, gap)
		}
	}

	// Started 2 s before 8:00 at one request per second: only two fit.
	g := newResearchFixture(t, tt.AtJST(2026, 10, 9, 7, 59, 58))
	g.settings.Nightly.RunTime = "01:00"
	for _, s := range []string{"1001", "1002", "1003", "1004"} {
		g.addSymbol(s, bar(s, "2026-10-08", 100))
	}
	g.runDue(t, g.newResearch())
	for _, at := range g.source.times {
		if h := at.In(time.FixedZone("JST", 9*3600)).Hour(); h >= 8 {
			t.Fatalf("a request left at %s", at)
		}
	}
	if len(g.source.calls) != 2 {
		t.Fatalf("requests = %v, want 2 before 08:00", g.source.calls)
	}
}
