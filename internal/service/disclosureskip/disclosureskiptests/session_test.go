package disclosureskiptests

import (
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/disclosureskip"
	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
)

// Trading-session time is the time News Ingest can fetch in: the lunch break,
// the evening, nights, weekends and holidays do not count. 2026-10-14 is a
// Wednesday; 10-16 a Friday; 10-19 the Monday after.
func TestSessionDeadline(t *testing.T) {
	closed := closedDays{{10, 15}: true} // a holiday on Thursday 10-15
	for _, tc := range []struct {
		name string
		from time.Time
		d    time.Duration
		want time.Time
	}{
		{"inside one session it is plain wall-clock time", jstAt(10, 14, 10, 0, 0), 60 * time.Minute, jstAt(10, 14, 11, 0, 0)},
		{"zero wait", jstAt(10, 14, 10, 0, 0), 0, jstAt(10, 14, 10, 0, 0)},
		{"late morning waits out the lunch break", jstAt(10, 14, 11, 25, 0), 60 * time.Minute, jstAt(10, 14, 13, 25, 0)},
		{"decision during the lunch break starts at 12:30", jstAt(10, 14, 12, 0, 0), 60 * time.Minute, jstAt(10, 14, 13, 30, 0)},
		{"right before the close waits for the next trading day (the 15th is a holiday)", jstAt(10, 14, 15, 27, 0), 60 * time.Minute, jstAt(10, 16, 9, 57, 0)},
		{"the close ends the day exactly", jstAt(10, 14, 14, 30, 0), 60 * time.Minute, jstAt(10, 14, 15, 30, 0)},
		{"Friday afternoon waits for Monday", jstAt(10, 16, 15, 20, 0), 60 * time.Minute, jstAt(10, 19, 9, 50, 0)},
		{"after the close waits for the next morning", jstAt(10, 14, 16, 0, 0), 60 * time.Minute, jstAt(10, 16, 10, 0, 0)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := disclosureskip.SessionDeadline(closed, tc.from, tc.d); !got.Equal(tc.want) {
				t.Fatalf("SessionDeadline = %s, want %s", got.In(marketcalendar.JST), tc.want.In(marketcalendar.JST))
			}
		})
	}
}

// SessionSince is the mirror of SessionDeadline, so the evaluator's look-back
// reaches exactly the decisions whose deadline is still ahead.
func TestSessionSinceMirrorsSessionDeadline(t *testing.T) {
	for _, deadline := range []time.Time{jstAt(10, 14, 13, 25, 0), jstAt(10, 14, 10, 30, 0), jstAt(10, 16, 9, 20, 0), jstAt(10, 14, 15, 30, 0)} {
		since := disclosureskip.SessionSince(weekdaysOnly, deadline, 60*time.Minute)
		if got := disclosureskip.SessionDeadline(weekdaysOnly, since, 60*time.Minute); !got.Equal(deadline) {
			t.Errorf("deadline %s: since = %s gives a deadline of %s", deadline, since, got)
		}
	}
	// Monday 09:20 looks back across the weekend to Friday afternoon.
	if got, want := disclosureskip.SessionSince(weekdaysOnly, jstAt(10, 19, 9, 20, 0), 60*time.Minute), jstAt(10, 16, 14, 50, 0); !got.Equal(want) {
		t.Errorf("SessionSince = %s, want %s", got, want)
	}
	// From the evening, the last session of the day is what counts.
	if got, want := disclosureskip.SessionSince(weekdaysOnly, jstAt(10, 14, 20, 0, 0), 60*time.Minute), jstAt(10, 14, 14, 30, 0); !got.Equal(want) {
		t.Errorf("SessionSince = %s, want %s", got, want)
	}
}

// The session boundaries the package keeps are marketcalendar.TSE's: a minute
// is a session minute for one exactly when it is for the other.
func TestSessionsAgreeWithTheTSECalendar(t *testing.T) {
	for _, day := range []int{14, 17} { // Wednesday and Saturday
		for minute := 0; minute < 24*60; minute++ {
			at := jstAt(10, day, minute/60, minute%60, 0)
			want := marketcalendar.TSE.IsOpen(at)
			// A 1-minute wait from a session minute ends one minute later iff it
			// stays inside the session; from a non-session minute it ends after the
			// next session opens, so it is more than a minute away.
			inside := disclosureskip.SessionDeadline(marketcalendar.TSE, at, time.Minute).Equal(at.Add(time.Minute))
			if inside != want {
				t.Fatalf("%s: package says in-session = %v, marketcalendar says %v", at, inside, want)
			}
		}
	}
}
