package researchindex_test

import (
	"slices"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/researchindex"
)

var jst = time.FixedZone("JST", 9*3600)

// Window is the last trading days (TSE calendar: no weekend, no holiday), up to
// today's only once its session is complete (15:35 JST).
func TestWindow(t *testing.T) {
	for _, tt := range []struct {
		name string
		now  time.Time
		n    int
		want []string
	}{
		{"after the session", time.Date(2026, 11, 20, 15, 35, 0, 0, jst), 3, []string{"2026-11-18", "2026-11-19", "2026-11-20"}},
		{"during the session", time.Date(2026, 11, 20, 15, 34, 0, 0, jst), 3, []string{"2026-11-17", "2026-11-18", "2026-11-19"}},
		{"a weekend", time.Date(2026, 11, 22, 12, 0, 0, 0, jst), 2, []string{"2026-11-19", "2026-11-20"}},
		// 11-23 (Mon) is Labor Thanksgiving Day: not a trading day.
		{"over a holiday", time.Date(2026, 11, 24, 16, 0, 0, 0, jst), 3, []string{"2026-11-19", "2026-11-20", "2026-11-24"}},
		{"none asked", time.Date(2026, 11, 20, 16, 0, 0, 0, jst), 0, nil},
	} {
		got := researchindex.Window(tt.now, tt.n)
		if !slices.Equal(got, tt.want) {
			t.Errorf("%s: Window = %v, want %v", tt.name, got, tt.want)
		}
	}
}

// SessionMinutes are the 330 minutes of 09:00-11:30 and 12:30-15:30; none on a
// holiday or for a malformed day.
func TestSessionMinutes(t *testing.T) {
	got := researchindex.SessionMinutes("2026-11-20")
	if len(got) != 330 || !got[0].Equal(time.Date(2026, 11, 20, 9, 0, 0, 0, jst)) || !got[149].Equal(time.Date(2026, 11, 20, 11, 29, 0, 0, jst)) ||
		!got[150].Equal(time.Date(2026, 11, 20, 12, 30, 0, 0, jst)) || !got[329].Equal(time.Date(2026, 11, 20, 15, 29, 0, 0, jst)) {
		t.Fatalf("SessionMinutes = %d minutes, first %v, last %v", len(got), got[0], got[len(got)-1])
	}
	for _, day := range []string{"2026-11-21", "2026-11-23", "x"} {
		if got := researchindex.SessionMinutes(day); got != nil {
			t.Errorf("SessionMinutes(%s) = %d minutes, want none", day, len(got))
		}
	}
}
