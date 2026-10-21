package healthalert_test

import (
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/healthalert"
)

// issue #863: a refused connection (kabuステーション not running) follows the
// rule of kabu_not_logged_in: at once at 08:45 when the streak was already
// there, otherwise after five minutes, with the streak's start as Since.
func TestKabuUnreachableAlertAfterFiveMinutes(t *testing.T) {
	r := newRig(at(20, 9, 0))
	r.jobs.last = at(20, 10, 0)
	r.token = healthalert.TokenState{UnreachableSince: at(20, 10, 0)}
	if got := r.check(t, at(20, 10, 4)); len(got) != 0 {
		t.Fatalf("after 4 minutes alerts = %v, want none", codesOf(got))
	}
	r.jobs.last = at(20, 10, 4)
	got := r.check(t, at(20, 10, 5))
	if len(got) != 1 || got[0].Code != domain.HealthAlertKabuUnreachable || !got[0].Since.Equal(at(20, 10, 0)) {
		t.Fatalf("after 5 minutes alerts = %+v, want kabu_unreachable since 10:00", got)
	}
	r.token = healthalert.TokenState{} // kabuステーション started and the token was issued
	r.jobs.last = at(20, 10, 5)
	if got = r.check(t, at(20, 10, 6)); len(got) != 0 {
		t.Fatalf("after recovery alerts = %v, want none (cleared automatically)", codesOf(got))
	}
}

func TestKabuUnreachableAlertAt0845WhenAlreadyRefusedSinceTheNight(t *testing.T) {
	r := newRig(at(20, 8, 0))
	r.token = healthalert.TokenState{UnreachableSince: at(19, 22, 47)}
	r.jobs.last = at(20, 8, 40)
	if got := r.check(t, at(20, 8, 44)); len(got) != 0 {
		t.Fatalf("08:44 alerts = %v, want none before the 08:45 check", codesOf(got))
	}
	got := r.check(t, at(20, 8, 45))
	if len(got) != 1 || got[0].Code != domain.HealthAlertKabuUnreachable || !got[0].Since.Equal(at(20, 8, 45)) {
		t.Fatalf("08:45 alerts = %+v, want kabu_unreachable since 08:45", got)
	}
}

// The two kabu alerts are separate codes: a not-logged-in streak never raises
// kabu_unreachable and the other way round.
func TestKabuUnreachableAndNotLoggedInAreSeparateCodes(t *testing.T) {
	r := newRig(at(20, 8, 0))
	r.jobs.last = at(20, 8, 40)
	r.token = healthalert.TokenState{NotLoggedInSince: at(20, 8, 0)}
	if got := codesOf(r.check(t, at(20, 8, 45))); len(got) != 1 || got[0] != domain.HealthAlertKabuNotLoggedIn {
		t.Fatalf("not logged in alerts = %v, want only kabu_not_logged_in", got)
	}
	r.token = healthalert.TokenState{UnreachableSince: at(20, 8, 0)}
	if got := codesOf(r.check(t, at(20, 8, 46))); len(got) != 1 || got[0] != domain.HealthAlertKabuUnreachable {
		t.Fatalf("refused alerts = %v, want only kabu_unreachable", got)
	}
}

func TestKabuUnreachableAlertOnlyInTheWatchWindow(t *testing.T) {
	r := newRig(at(24, 8, 0))
	r.token = healthalert.TokenState{UnreachableSince: at(23, 20, 0)}
	for _, now := range []time.Time{at(24, 8, 45), at(24, 10, 0), at(12, 10, 0)} { // Saturday, holiday
		if got := r.check(t, now); len(got) != 0 {
			t.Errorf("non-trading day %v alerts = %v, want none", now, codesOf(got))
		}
	}
	r.token = healthalert.TokenState{UnreachableSince: at(19, 20, 0)}
	for _, now := range []time.Time{at(20, 3, 0), at(20, 8, 44), at(20, 15, 30), at(20, 22, 0)} {
		if got := r.check(t, now); len(got) != 0 {
			t.Errorf("trading day outside the hours %v alerts = %v, want none", now, codesOf(got))
		}
	}
	if got := r.check(t, at(20, 12, 0)); len(got) != 1 || got[0].Code != domain.HealthAlertKabuUnreachable {
		t.Errorf("lunch break alerts = %v, want kabu_unreachable", codesOf(got))
	}
}

func TestKabuUnreachableSince(t *testing.T) {
	open := at(20, 8, 45)
	tests := []struct {
		name       string
		now        time.Time
		streak     time.Time
		wantSince  time.Time
		wantRaised bool
	}{
		{"already refused at 08:45 raises at once", at(20, 8, 45), at(19, 22, 47), open, true},
		{"a streak after 08:45 waits five minutes", at(20, 10, 4), at(20, 10, 0), time.Time{}, false},
		{"... and is raised at five minutes", at(20, 10, 5), at(20, 10, 0), at(20, 10, 0), true},
		{"nothing at the close", at(20, 15, 30), at(20, 8, 0), time.Time{}, false},
		{"nothing without a streak", at(20, 10, 0), time.Time{}, time.Time{}, false},
		{"nothing on a Saturday", at(24, 10, 0), at(24, 8, 0), time.Time{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			since, ok := healthalert.KabuUnreachableSince(tt.now, tt.streak)
			if ok != tt.wantRaised || !since.Equal(tt.wantSince) {
				t.Errorf("KabuUnreachableSince(%v, %v) = %v, %v; want %v, %v", tt.now, tt.streak, since, ok, tt.wantSince, tt.wantRaised)
			}
		})
	}
}

func TestInWindowKabuUnreachable(t *testing.T) {
	code := domain.HealthAlertKabuUnreachable
	for now, want := range map[time.Time]bool{
		at(20, 8, 44): false, at(20, 8, 45): true, at(20, 12, 0): true, at(20, 15, 30): false, at(24, 10, 0): false,
	} {
		if got := healthalert.InWindow(code, now); got != want {
			t.Errorf("InWindow(%q, %v) = %v, want %v", code, now, got, want)
		}
	}
}
