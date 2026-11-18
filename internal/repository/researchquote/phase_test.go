package researchquote

import (
	"testing"
	"time"
)

func TestPhaseAt(t *testing.T) {
	jst := time.FixedZone("JST", 9*3600)
	for _, tc := range []struct {
		hh, mm int
		want   string
	}{
		{9, 0, PhaseOpenAuction}, {9, 4, PhaseOpenAuction}, {9, 5, PhaseMorning}, {11, 24, PhaseMorning},
		{11, 25, PhaseLunch}, {12, 34, PhaseLunch}, {12, 35, PhaseAfternoon}, {15, 19, PhaseAfternoon}, {15, 20, PhaseClose}, {15, 29, PhaseClose},
	} {
		if got := PhaseAt(time.Date(2026, 10, 8, tc.hh, tc.mm, 0, 0, jst)); got != tc.want {
			t.Errorf("%02d:%02d = %s, want %s", tc.hh, tc.mm, got, tc.want)
		}
	}
	// The zone is JST whatever the instant's own: 00:30 UTC is 09:30 JST.
	if got := PhaseAt(time.Date(2026, 10, 8, 0, 30, 0, 0, time.UTC)); got != PhaseMorning {
		t.Errorf("00:30 UTC = %s, want morning", got)
	}
}
