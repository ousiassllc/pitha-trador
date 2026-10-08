package rankingwatch_test

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap/rankingwatch"
)

var t0 = time.Date(2026, 10, 6, 10, 0, 0, 0, time.UTC)

func codes(prefix string, from, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("%s%03d", prefix, from+i)
	}
	return out
}

func TestSelector_FillsFreeSlotsAtOnceUpToTheCap(t *testing.T) {
	var s rankingwatch.Selector
	watch, added, removed := s.Update(t0, nil, codes("R", 0, 60))
	if len(watch) != rankingwatch.DefaultMaxWatched || added != rankingwatch.DefaultMaxWatched || removed != 0 {
		t.Fatalf("watch=%d added=%d removed=%d, want %d/%d/0", len(watch), added, removed, rankingwatch.DefaultMaxWatched, rankingwatch.DefaultMaxWatched)
	}
	for _, sym := range codes("R", 0, rankingwatch.DefaultMaxWatched) { // the best-ranked ones
		if !slices.Contains(watch, sym) {
			t.Errorf("%s missing from the watch list", sym)
		}
	}
}

func TestSelector_HeldSymbolsTakeFixedSlots(t *testing.T) {
	var s rankingwatch.Selector
	held := []string{"H1", "H2", "H3"}
	watch, _, _ := s.Update(t0, held, codes("R", 0, 60))
	if len(watch) != rankingwatch.DefaultMaxWatched {
		t.Fatalf("watch = %d symbols, want the cap %d", len(watch), rankingwatch.DefaultMaxWatched)
	}
	if !slices.Equal(watch[:3], held) {
		t.Errorf("watch starts %v, want the held symbols %v", watch[:3], held)
	}
	// A held symbol that is also ranked counts once, in its fixed slot.
	watch, _, _ = s.Update(t0.Add(time.Minute), []string{"H1", "R000"}, codes("R", 0, 60))
	if n := count(watch, "R000"); n != 1 {
		t.Errorf("R000 appears %d times, want 1", n)
	}
	if len(watch) != rankingwatch.DefaultMaxWatched {
		t.Errorf("watch = %d symbols, want %d", len(watch), rankingwatch.DefaultMaxWatched)
	}
}

func count(symbols []string, sym string) int {
	n := 0
	for _, s := range symbols {
		if s == sym {
			n++
		}
	}
	return n
}

func TestSelector_NewHeldSymbolEvictsRankedOneImmediately(t *testing.T) {
	var s rankingwatch.Selector
	s.Update(t0, nil, codes("R", 0, 45))
	watch, _, removed := s.Update(t0.Add(time.Minute), []string{"H1"}, codes("R", 0, 45))
	if !slices.Contains(watch, "H1") || len(watch) != rankingwatch.DefaultMaxWatched || removed != 1 {
		t.Fatalf("watch=%d removed=%d has H1=%v, want a full list with one ranked symbol evicted for H1",
			len(watch), removed, slices.Contains(watch, "H1"))
	}
}

func TestSelector_MinHoldKeepsNewSymbolsForFiveMinutes(t *testing.T) {
	var s rankingwatch.Selector
	first := codes("A", 0, rankingwatch.DefaultMaxWatched)
	s.Update(t0, nil, first)

	fresh := codes("B", 0, rankingwatch.DefaultMaxWatched) // the whole ranking changes
	for _, after := range []time.Duration{time.Minute, 4 * time.Minute} {
		watch, added, removed := s.Update(t0.Add(after), nil, fresh)
		if added != 0 || removed != 0 || !slices.Equal(sorted(watch), sorted(first)) {
			t.Fatalf("after %v: added=%d removed=%d, want the list unchanged inside the 5 minute hold", after, added, removed)
		}
	}
}

func TestSelector_ReplacesAtMostFivePerCycleAfterMinHold(t *testing.T) {
	var s rankingwatch.Selector
	s.Update(t0, nil, codes("A", 0, rankingwatch.DefaultMaxWatched))

	fresh := codes("B", 0, rankingwatch.DefaultMaxWatched)
	now := t0.Add(rankingwatch.MinHold)
	watch, added, removed := s.Update(now, nil, fresh)
	if added != rankingwatch.MaxReplacePerCycle || removed != rankingwatch.MaxReplacePerCycle {
		t.Fatalf("added=%d removed=%d, want %d each", added, removed, rankingwatch.MaxReplacePerCycle)
	}
	if len(watch) != rankingwatch.DefaultMaxWatched {
		t.Fatalf("watch = %d symbols, want %d", len(watch), rankingwatch.DefaultMaxWatched)
	}
	// The symbols put in a minute ago are held for five minutes too: the next
	// cycle replaces five more of the original ones, not the new ones.
	watch, _, _ = s.Update(now.Add(time.Minute), nil, fresh)
	if n := countPrefix(watch, "B"); n != 2*rankingwatch.MaxReplacePerCycle {
		t.Errorf("B symbols after two cycles = %d, want %d", n, 2*rankingwatch.MaxReplacePerCycle)
	}
}

func countPrefix(symbols []string, prefix string) int {
	n := 0
	for _, s := range symbols {
		if s[:1] == prefix {
			n++
		}
	}
	return n
}

func sorted(s []string) []string {
	out := slices.Clone(s)
	slices.Sort(out)
	return out
}

func TestSelector_EmptyRankingDropsRankedSymbolsAndKeepsHeldOnes(t *testing.T) {
	var s rankingwatch.Selector
	s.Update(t0, []string{"H1"}, codes("R", 0, 30))

	watch, added, removed := s.Update(t0.Add(time.Minute), []string{"H1"}, nil)
	if !slices.Equal(watch, []string{"H1"}) || added != 0 || removed != 30 {
		t.Fatalf("watch=%v added=%d removed=%d, want only H1 with all 30 ranked symbols removed (no stale ones, no min-hold)", watch, added, removed)
	}
	// Recovery: the next ranking fills the slots again.
	watch, added, _ = s.Update(t0.Add(2*time.Minute), []string{"H1"}, codes("R", 0, 30))
	if len(watch) != 31 || added != 30 {
		t.Fatalf("watch=%d added=%d, want 31/30 after the ranking is back", len(watch), added)
	}
}
