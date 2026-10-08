package rankingwatch_test

import (
	"slices"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap/rankingwatch"
)

func TestSelector_RetainKeepsRankedSymbolsForScreeningAndHeldOnlyForWatch(t *testing.T) {
	var s rankingwatch.Selector
	s.Update(t0, []string{"H1"}, []string{"R1", "R2"})

	watch, screen := s.Retain([]string{"H1"})

	if !slices.Equal(watch, []string{"H1"}) {
		t.Errorf("watch = %v, want the held symbol only", watch)
	}
	if !slices.Equal(screen, []string{"H1", "R1", "R2"}) {
		t.Errorf("screen = %v, want held then the retained ranked symbols", screen)
	}
}

func TestSelector_RetainMovesNewlyHeldSymbolsToFixedSlotsAndCapsTheList(t *testing.T) {
	var s rankingwatch.Selector
	s.Update(t0, nil, codes("R", 0, 60))
	held := codes("H", 0, 3)
	held = append(held, "R000")

	watch, screen := s.Retain(held)

	if !slices.Equal(watch, held) {
		t.Errorf("watch = %v, want the held symbols %v", watch, held)
	}
	if len(screen) != rankingwatch.DefaultMaxWatched {
		t.Fatalf("screen = %d symbols, want the cap %d", len(screen), rankingwatch.DefaultMaxWatched)
	}
	if n := count(screen, "R000"); n != 1 {
		t.Errorf("R000 appears %d times, want 1 (its fixed held slot)", n)
	}
}

// The MinHold clock of the retained ranked symbols keeps running across the
// break: a symbol put in at t0 is replaceable right after the lunch break.
func TestSelector_RetainKeepsMinHoldTimestamps(t *testing.T) {
	var s rankingwatch.Selector
	full := codes("R", 0, rankingwatch.DefaultMaxWatched)
	s.Update(t0, nil, full)
	s.Retain(nil)

	reopen := t0.Add(time.Hour)
	ranking := append(codes("N", 0, 5), full...) // five newcomers outrank the retained ones
	watch, added, removed := s.Update(reopen, nil, ranking)

	if added != rankingwatch.MaxReplacePerCycle || removed != rankingwatch.MaxReplacePerCycle {
		t.Errorf("added=%d removed=%d, want %d each (the retained symbols are older than MinHold)", added, removed, rankingwatch.MaxReplacePerCycle)
	}
	if countPrefix(watch, "N") != rankingwatch.MaxReplacePerCycle {
		t.Errorf("watch has %d newcomers, want %d", countPrefix(watch, "N"), rankingwatch.MaxReplacePerCycle)
	}
}
