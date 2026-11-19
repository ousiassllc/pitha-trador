package researchquotes

import (
	"slices"
	"testing"
)

func TestPlannerKabuFirstThenExtrasDedupedAndCapped(t *testing.T) {
	kabu := symbolsN("K", 50)
	extras := append([]string{"K000", "7203", "bad code!", ""}, symbolsN("X", 100)...)

	var p planner
	got := p.update("2026-10-08", kabu, extras, 200) // the hard cap (120) wins over a larger setting
	if len(got) != 120 {
		t.Fatalf("plan has %d symbols, want the hard cap 120", len(got))
	}
	if !slices.Equal(got[:50], kabu) {
		t.Errorf("the first 50 are not kabu's, in order: %v", got[:50])
	}
	if got[50] != "7203" || got[51] != "X000" {
		t.Errorf("extras start %v %v, want the deduplicated 7203 then X000 (K000 repeated, malformed dropped)", got[50], got[51])
	}

	var q planner
	if got := q.update("2026-10-08", kabu, extras, 60); len(got) != 60 || !slices.Equal(got[:50], kabu) {
		t.Errorf("max_symbols 60: %d symbols, want 60 with kabu's 50 first", len(got))
	}
	var z planner
	if got := z.update("2026-10-08", kabu, extras, 30); len(got) != 30 || !slices.Equal(got, kabu[:30]) {
		t.Errorf("max_symbols 30 below kabu's count: %v, want kabu's first 30 (kabu first)", got)
	}
}

func TestPlannerIsStickyWithinTheDayAndStartsOverTheNext(t *testing.T) {
	var p planner
	first := slices.Clone(p.update("2026-10-08", []string{"A", "B", "C"}, []string{"X"}, 10))
	// kabu's list changes: B is gone, D is new, the order differs.
	second := slices.Clone(p.update("2026-10-08", []string{"D", "A", "C"}, []string{"X"}, 10))
	if want := append(slices.Clone(first), "D"); !slices.Equal(second, want) {
		t.Errorf("second plan = %v, want the first %v plus the new D at the end (nothing removed or reordered)", second, first)
	}
	if third := p.update("2026-10-08", []string{"D", "A", "C"}, []string{"X"}, 10); !slices.Equal(third, second) {
		t.Errorf("an unchanged input changed the plan: %v", third)
	}
	if next := p.update("2026-10-09", []string{"D"}, nil, 10); !slices.Equal(next, []string{"D"}) {
		t.Errorf("next day's plan = %v, want a fresh [D]", next)
	}
}
