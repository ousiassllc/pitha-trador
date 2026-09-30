package domain_test

import (
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// TestSystemState_AllowedActions pins the transition table the UI derives
// its buttons from (docs/api/endpoints.md §4): Pause only from Running,
// Resume from Paused/Killed, Kill from Running/Paused; an unknown state
// allows nothing.
func TestSystemState_AllowedActions(t *testing.T) {
	tests := []struct {
		state                        domain.SystemState
		canPause, canResume, canKill bool
	}{
		{domain.SystemStateRunning, true, false, true},
		{domain.SystemStatePaused, false, true, true},
		{domain.SystemStateKilled, false, true, false},
		{"", false, false, false},
	}
	for _, tc := range tests {
		if got := tc.state.CanPause(); got != tc.canPause {
			t.Errorf("%q.CanPause() = %v, want %v", tc.state, got, tc.canPause)
		}
		if got := tc.state.CanResume(); got != tc.canResume {
			t.Errorf("%q.CanResume() = %v, want %v", tc.state, got, tc.canResume)
		}
		if got := tc.state.CanKill(); got != tc.canKill {
			t.Errorf("%q.CanKill() = %v, want %v", tc.state, got, tc.canKill)
		}
	}
}
