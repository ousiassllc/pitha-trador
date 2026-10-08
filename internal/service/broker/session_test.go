package broker_test

import (
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/broker"
)

func TestSessionStatus_Persistent(t *testing.T) {
	now := time.Date(2026, 10, 8, 9, 0, 0, 0, time.UTC)
	cases := []struct {
		name        string
		status      broker.SessionStatus
		wantPersist bool
		wantElapsed time.Duration
	}{
		{"healthy", broker.SessionStatus{}, false, 0},
		{"not_logged_in below both thresholds", broker.SessionStatus{Issue: broker.SessionIssueNotLoggedIn, Failures: 4, Since: now.Add(-4 * time.Minute)}, false, 4 * time.Minute},
		{"not_logged_in 5 failures", broker.SessionStatus{Issue: broker.SessionIssueNotLoggedIn, Failures: 5, Since: now.Add(-time.Minute)}, true, time.Minute},
		{"not_logged_in 5 minutes", broker.SessionStatus{Issue: broker.SessionIssueNotLoggedIn, Failures: 2, Since: now.Add(-5 * time.Minute)}, true, 5 * time.Minute},
		{"not_logged_in without a start time", broker.SessionStatus{Issue: broker.SessionIssueNotLoggedIn, Failures: 9}, false, 0},
		{"other issues are never persistent", broker.SessionStatus{Issue: broker.SessionIssueBadPassword, Failures: 9, Since: now.Add(-time.Hour)}, false, 0},
		{"clock skew clamps elapsed to zero", broker.SessionStatus{Issue: broker.SessionIssueNotLoggedIn, Failures: 5, Since: now.Add(time.Minute)}, true, 0},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			persistent, elapsed := tt.status.Persistent(now)
			if persistent != tt.wantPersist || elapsed != tt.wantElapsed {
				t.Errorf("Persistent = %v, %v; want %v, %v", persistent, elapsed, tt.wantPersist, tt.wantElapsed)
			}
		})
	}
}
