package rankingwatch_test

import (
	"context"
	"slices"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap/rankingwatch"
	"github.com/ousiassllc/pitha-trador/internal/config/watchsettings"
)

func universeOf(n int) []string { return codes("U", 0, n) }

func (r *rig) useSettings(set watchsettings.Settings) {
	r.w.Settings = func(context.Context) watchsettings.Settings { return set }
}

// issue #1055: with no Settings (or the default ones) the watch list is the
// broker's cap, exactly as before.
func TestWatcher_DefaultSettingsKeepTheFullBrokerCap(t *testing.T) {
	for name, apply := range map[string]func(*rig){
		"no Settings":      func(*rig) {},
		"default Settings": func(r *rig) { r.useSettings(watchsettings.Default()) },
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t, universeOf(80)...)
			r.w.MaxWatched = 45 // the kabu adapter's broker.Capabilities.MaxStreamSymbols
			apply(r)
			r.src.set(universeOf(80), nil, false)
			r.cycle()
			if got := r.list.Len(); got != 45 || len(r.reg.sets[0]) != 45 {
				t.Errorf("watch = %d, registered = %d, want 45", got, len(r.reg.sets[0]))
			}
		})
	}
}

func TestWatcher_SettingsLowerTheListSizeFromTheNextCycle(t *testing.T) {
	r := newRig(t, universeOf(80)...)
	r.w.MaxWatched = 45
	set := watchsettings.Default()
	r.w.Settings = func(context.Context) watchsettings.Settings { return set }
	r.src.set(universeOf(80), nil, false)
	r.cycle()
	if r.list.Len() != 45 {
		t.Fatalf("watch = %d, want 45", r.list.Len())
	}
	set.MaxWatched = 20 // lowered after a burst of 429 / 4001006
	r.cycle()
	if r.list.Len() != 20 {
		t.Errorf("watch = %d after lowering to 20, want 20", r.list.Len())
	}
	if last := r.reg.sets[len(r.reg.sets)-1]; len(last) != 20 {
		t.Errorf("registered = %d, want 20", len(last))
	}
}

// A value above the broker's cap can never get through the Watcher either,
// even if it were stored: the list is clamped to the stream cap.
func TestWatcher_SettingsCannotRaiseTheListAboveTheBrokerCap(t *testing.T) {
	r := newRig(t, universeOf(200)...)
	r.w.MaxWatched = 45
	r.useSettings(watchsettings.Settings{MaxWatched: 120})
	r.src.set(universeOf(200), nil, false)
	r.cycle()
	if got := r.list.Len(); got != 45 {
		t.Errorf("watch = %d, want the broker cap 45", got)
	}
	r2 := newRig(t, universeOf(200)...) // no broker cap given: DefaultMaxWatched
	r2.useSettings(watchsettings.Settings{MaxWatched: 120})
	r2.src.set(universeOf(200), nil, false)
	r2.cycle()
	if got := r2.list.Len(); got != rankingwatch.DefaultMaxWatched {
		t.Errorf("watch = %d, want DefaultMaxWatched %d", got, rankingwatch.DefaultMaxWatched)
	}
}

func TestWatcher_ObservationSymbolsAreOffByDefaultAndTakeSlotsWithinTheCap(t *testing.T) {
	r := newRig(t, universeOf(80)...)
	r.w.MaxWatched = 45
	// Listed but switched off: nothing changes.
	r.useSettings(watchsettings.Settings{MaxWatched: 45, ObservationSymbols: []string{"U070", "U071"}})
	r.src.set(universeOf(40), nil, false)
	r.cycle()
	if r.list.Contains("U070") || r.list.Len() != 40 {
		t.Fatalf("switch off: watch = %d, contains U070 = %v; want the ranking only", r.list.Len(), r.list.Contains("U070"))
	}

	// On: the observation symbols are watched even though the ranking does not
	// carry them, and the list still never exceeds the size.
	r.useSettings(watchsettings.Settings{MaxWatched: 45, ObservationEnabled: true, ObservationSymbols: []string{"U070", "U071", "NOTINUNIVERSE"}})
	r.src.set(universeOf(80), nil, false)
	r.cycle()
	if !r.list.Contains("U070") || !r.list.Contains("U071") || r.list.Contains("NOTINUNIVERSE") {
		t.Errorf("observation symbols not watched: U070=%v U071=%v", r.list.Contains("U070"), r.list.Contains("U071"))
	}
	if got := r.list.Len(); got != 45 {
		t.Errorf("watch = %d, want the cap 45 (observation symbols come out of it)", got)
	}
	if got := len(r.lastEnqueued()); got < 45 {
		t.Errorf("enqueued = %d, want the market data of every watched symbol", got)
	}
}

// Held symbols come first: observation symbols are the first to give way.
func TestWatcher_ObservationSymbolsYieldToHeldSymbols(t *testing.T) {
	r := newRig(t, universeOf(10)...)
	r.w.MaxWatched = 3
	r.held.symbols = []string{"U000", "U001"}
	r.useSettings(watchsettings.Settings{MaxWatched: 3, ObservationEnabled: true, ObservationSymbols: []string{"U005", "U006"}})
	r.src.set([]string{"U007"}, nil, false)
	r.cycle()
	watch := r.reg.sets[0]
	if len(watch) != 3 || !slices.Contains(watch, "U000") || !slices.Contains(watch, "U001") || !slices.Contains(watch, "U005") || slices.Contains(watch, "U006") {
		t.Errorf("watch = %v, want both held + the first observation symbol only", watch)
	}
}

// A Settings size below the number of held symbols never drops a held symbol
// from the watch list (its price feeds the position's exit).
func TestWatcher_LoweredSizeNeverDropsAHeldSymbol(t *testing.T) {
	r := newRig(t, universeOf(20)...)
	r.w.MaxWatched = 45
	r.held.symbols = []string{"U000", "U001", "U002", "U003", "U004", "U005", "U006", "U007"}
	r.useSettings(watchsettings.Settings{MaxWatched: 5})
	r.src.set([]string{"U010", "U011"}, nil, false)
	r.cycle()
	watch := r.reg.sets[0]
	for _, sym := range r.held.symbols {
		if !slices.Contains(watch, sym) {
			t.Errorf("held %s missing from the watch list %v", sym, watch)
		}
	}
	if len(watch) != len(r.held.symbols) {
		t.Errorf("watch = %d symbols, want exactly the 8 held ones (no ranked symbol fits)", len(watch))
	}
}

// Outside the session only the held symbols are registered for PUSH and
// ingested; an observation-only symbol (issue #1055) is not held and takes no
// slot, however the switch is set, and comes back with the next session.
func TestWatcher_ObservationSymbolsAreNotWatchedOutsideTheSession(t *testing.T) {
	r := newRig(t, universeOf(10)...)
	r.w.MaxWatched = 45
	r.held.symbols = []string{"U000"}
	r.useSettings(watchsettings.Settings{MaxWatched: 45, ObservationEnabled: true, ObservationSymbols: []string{"U005"}})
	r.src.set([]string{"U001"}, nil, false)
	r.cycle()
	if got := r.reg.sets[len(r.reg.sets)-1]; !slices.Contains(got, "U005") {
		t.Fatalf("setup: in the session the observation symbol must be watched, registered = %v", got)
	}

	open := r.w.Open
	r.w.Open = closed
	r.cycle()
	if last := r.reg.sets[len(r.reg.sets)-1]; !slices.Equal(last, []string{"U000"}) {
		t.Errorf("registered = %v outside the session, want the held symbol only", last)
	}
	if got := r.lastEnqueued(); !slices.Equal(got, []string{"U000"}) {
		t.Errorf("enqueued = %v outside the session, want the held symbol only (no market data for U005)", got)
	}
	if r.list.Contains("U005") {
		t.Error("the observation symbol is on the candidate list outside the session")
	}

	r.w.Open = open
	r.src.set([]string{"U001"}, nil, false)
	r.cycle()
	if got := r.reg.sets[len(r.reg.sets)-1]; !slices.Contains(got, "U005") {
		t.Errorf("registered = %v in the next session, want the observation symbol back", got)
	}
}
