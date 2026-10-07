package candidates

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/jobqueue"
)

func scoutJobCount(t *testing.T, r *Refresher, since time.Time) int {
	t.Helper()
	jobs, err := r.Jobs.ListOpenOrFinishedSince(context.Background(), jobqueue.JobQueueJevScout, since.Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("ListOpenOrFinishedSince: %v", err)
	}
	return len(jobs)
}

func scanSymbol(t *testing.T, r *Refresher, symbol string) domain.ScanSymbol {
	t.Helper()
	cycle, ok, err := r.Screener.Scan(context.Background())
	if err != nil || !ok {
		t.Fatalf("Scan: ok=%v err=%v", ok, err)
	}
	for _, s := range cycle.Symbols {
		if s.Symbol == symbol {
			return s
		}
	}
	t.Fatalf("symbol %q missing from scan cycle", symbol)
	return domain.ScanSymbol{}
}

// Issue #685: a bar left from the previous session must not reach Jev
// Scout when the session reopens (#668 retained list), but the symbol
// stays visible in the Scanner with stale_snapshot; once market-data
// writes a fresh bar it is scouted.
func TestRefresh_StaleBarKeptVisibleButNotScouted(t *testing.T) {
	refresher, inst, clock := newScoutCooldownRefresher(t)
	ctx := context.Background()
	// Session reopens the next morning: the stored bar is ~17h old.
	*clock = clock.Add(17 * time.Hour)

	if err := refresher.Refresh(ctx); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if n := scoutJobCount(t, refresher, *clock); n != 0 {
		t.Fatalf("jev-scout jobs = %d on a stale bar, want 0", n)
	}
	s := scanSymbol(t, refresher, "7203")
	if !s.Reasons.Has(domain.ScreenReasonStaleSnapshot) || s.Status() != domain.ScanStatusMissing {
		t.Errorf("reasons = %v status = %v, want stale_snapshot / missing", s.Reasons.List(), s.Status())
	}
	if cands, _, _ := refresher.Screener.Candidates(ctx); len(cands) != 0 {
		t.Errorf("candidates = %d, want 0 for a stale bar", len(cands))
	}

	insertFreshBar(t, refresher, inst, *clock)
	if err := refresher.Refresh(ctx); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if n := scoutJobCount(t, refresher, *clock); n != 1 {
		t.Fatalf("jev-scout jobs = %d after a fresh bar, want 1", n)
	}
	if s := scanSymbol(t, refresher, "7203"); s.Reasons.Has(domain.ScreenReasonStaleSnapshot) {
		t.Errorf("stale_snapshot still set after a fresh bar: %v", s.Reasons.List())
	}
}

// The staleness boundary is domain.MaxSnapshotAge: exactly that old is
// still fresh, one nanosecond more is stale.
func TestRefresh_StaleBoundary(t *testing.T) {
	for _, tc := range []struct {
		name      string
		age       time.Duration
		wantStale bool
	}{
		{"exactly max age", domain.MaxSnapshotAge, false},
		{"just over max age", domain.MaxSnapshotAge + time.Nanosecond, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			refresher, _, clock := newScoutCooldownRefresher(t)
			*clock = clock.Add(tc.age)
			if err := refresher.Refresh(context.Background()); err != nil {
				t.Fatalf("Refresh: %v", err)
			}
			if got := scanSymbol(t, refresher, "7203").Reasons.Has(domain.ScreenReasonStaleSnapshot); got != tc.wantStale {
				t.Errorf("stale_snapshot = %v, want %v", got, tc.wantStale)
			}
			wantJobs := 1
			if tc.wantStale {
				wantJobs = 0
			}
			if n := scoutJobCount(t, refresher, *clock); n != wantJobs {
				t.Errorf("jev-scout jobs = %d, want %d", n, wantJobs)
			}
		})
	}
}

// Off-session (#668) the retained list is still shown from stored data
// without a stale mark: the staleness gate applies only in session.
func TestRefresh_OffSessionDoesNotMarkStale(t *testing.T) {
	refresher, _, clock := newScoutCooldownRefresher(t)
	refresher.InSession = func(time.Time) bool { return false }
	*clock = clock.Add(17 * time.Hour)
	if err := refresher.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	s := scanSymbol(t, refresher, "7203")
	if s.Reasons.Has(domain.ScreenReasonStaleSnapshot) || s.Status() != domain.ScanStatusPassed {
		t.Errorf("reasons = %v status = %v, want passed off-session", s.Reasons.List(), s.Status())
	}
}
