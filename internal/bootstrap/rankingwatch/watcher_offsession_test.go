package rankingwatch_test

import (
	"slices"
	"testing"
	"time"
)

func closed(time.Time) bool { return false }

// Outside the session no ranking is requested and PUSH registration /
// ingestion shrink to the held symbols, but the candidate list keeps showing
// the last watch list (issue #668, FR-SCAN-7, non-functional.md §3).
func TestWatcher_OutsideTheSessionRequestsNoRankingAndKeepsTheLastCandidateList(t *testing.T) {
	r := newRig(t, "7203", "6758", "9984")
	r.held.symbols = []string{"6758"}
	r.src.set([]string{"7203", "9984"}, nil, false)
	r.cycle()
	if r.list.Len() != 3 {
		t.Fatalf("setup: watch list = %d symbols, want 3", r.list.Len())
	}
	calls := r.src.calls

	r.w.Open = closed // 大引け後・昼休み・土日祝
	r.cycle()

	if r.src.calls != calls {
		t.Errorf("ranking requests = %d, want none outside the session", r.src.calls-calls)
	}
	for _, sym := range []string{"7203", "6758", "9984"} {
		if !r.list.Contains(sym) {
			t.Errorf("candidate list lost %s outside the session", sym)
		}
	}
	if last := r.reg.sets[len(r.reg.sets)-1]; !slices.Equal(last, []string{"6758"}) {
		t.Errorf("registered = %v, want the held symbol only outside the session", last)
	}
	if got := r.lastEnqueued(); !slices.Equal(got, []string{"6758"}) {
		t.Errorf("enqueued = %v, want the held symbol only outside the session", got)
	}
}

func TestWatcher_OutsideTheSessionAlwaysIncludesHeldSymbols(t *testing.T) {
	r := newRig(t, "7203", "6758")
	r.src.set([]string{"7203"}, nil, false)
	r.cycle()
	r.w.Open = closed
	r.held.symbols = []string{"6758"} // an order placed meanwhile
	r.cycle()
	if !r.list.Contains("7203") || !r.list.Contains("6758") {
		t.Errorf("candidate list = 7203:%v 6758:%v, want the retained ranked symbol and the held one", r.list.Contains("7203"), r.list.Contains("6758"))
	}
}

// Without a previous in-session cycle (restart outside the session) only the
// held symbols are known.
func TestWatcher_OutsideTheSessionAfterRestartListsHeldOnly(t *testing.T) {
	r := newRig(t, "7203", "6758")
	r.held.symbols = []string{"6758"}
	r.w.Open = closed
	r.src.set([]string{"7203"}, nil, false)
	r.cycle()
	if r.src.calls != 0 {
		t.Errorf("ranking requests = %d, want 0 outside the session", r.src.calls)
	}
	if !r.list.Contains("6758") || r.list.Contains("7203") || r.list.Len() != 1 {
		t.Error("with no previous list only the held symbol should be listed")
	}
}

// The retained ranked symbols keep their MinHold clock across the break, and
// reopening resumes the ranking normally.
func TestWatcher_ReopeningResumesTheRankingFromTheRetainedList(t *testing.T) {
	r := newRig(t, "7203", "6758")
	r.src.set([]string{"7203"}, nil, false)
	r.cycle()
	open := r.w.Open
	r.w.Open = closed
	r.cycle()
	r.w.Open = open
	r.src.set([]string{"7203", "6758"}, nil, false)
	r.cycle()
	if !r.list.Contains("7203") || !r.list.Contains("6758") {
		t.Error("ranking after reopening should list both symbols")
	}
	if got := sorted(r.lastEnqueued()); !slices.Equal(got, []string{"6758", "7203"}) {
		t.Errorf("enqueued = %v, want both symbols after reopening", got)
	}
}
