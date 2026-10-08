package kabu_test

import (
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/broker"
	"github.com/ousiassllc/pitha-trador/internal/service/marketdata"
	"github.com/ousiassllc/pitha-trador/internal/service/marketdata/kabu"
)

func TestSessionStatusOf_MapsEveryTokenIssue(t *testing.T) {
	tests := []struct {
		issue marketdata.TokenIssue
		want  broker.SessionIssue
	}{
		{marketdata.TokenIssueNone, broker.SessionIssueNone},
		{marketdata.TokenIssueUnreachable, broker.SessionIssueUnreachable},
		{marketdata.TokenIssueNotLoggedIn, broker.SessionIssueNotLoggedIn},
		{marketdata.TokenIssueAPIDisabled, broker.SessionIssueAPIDisabled},
		{marketdata.TokenIssueBadPassword, broker.SessionIssueBadPassword},
		{marketdata.TokenIssueUnknown, broker.SessionIssueUnknown},
		{marketdata.TokenIssueRejected, broker.SessionIssueRejected},
	}
	for _, tt := range tests {
		ts := marketdata.TokenStatus{Issue: tt.issue, Code: 4001007, Failures: 3, Since: time.Unix(100, 0)}
		got := kabu.SessionStatusOf(ts)
		if got.Issue != tt.want || got.Code != 4001007 || got.Failures != 3 || !got.Since.Equal(ts.Since) {
			t.Errorf("SessionStatusOf(%q) = %+v, want issue %q with code/streak carried over", tt.issue, got, tt.want)
		}
		if got.Guidance != ts.Guidance() {
			t.Errorf("SessionStatusOf(%q).Guidance = %q, want kabu's %q", tt.issue, got.Guidance, ts.Guidance())
		}
	}
	if g := kabu.SessionStatusOf(marketdata.TokenStatus{Issue: marketdata.TokenIssueBadPassword}).Guidance; !strings.Contains(g, "KABU_API_PASSWORD") {
		t.Errorf("bad_password guidance = %q, want the kabu wording naming KABU_API_PASSWORD", g)
	}
}

// issue #712: only a not_logged_in streak escalates, after 5 failures or 5 minutes.
func TestSessionStatusOf_Persistent(t *testing.T) {
	now := time.Date(2026, 10, 8, 9, 0, 0, 0, time.UTC)
	tests := []struct {
		name        string
		status      marketdata.TokenStatus
		wantPersist bool
		wantElapsed time.Duration
	}{
		{name: "no failure", status: marketdata.TokenStatus{}},
		{name: "few recent failures", status: marketdata.TokenStatus{Issue: marketdata.TokenIssueNotLoggedIn, Failures: 4, Since: now.Add(-2 * time.Minute)}, wantElapsed: 2 * time.Minute},
		{name: "repeated failures", status: marketdata.TokenStatus{Issue: marketdata.TokenIssueNotLoggedIn, Failures: 5, Since: now.Add(-3 * time.Minute)}, wantPersist: true, wantElapsed: 3 * time.Minute},
		{name: "long lasting", status: marketdata.TokenStatus{Issue: marketdata.TokenIssueNotLoggedIn, Failures: 2, Since: now.Add(-5 * time.Minute)}, wantPersist: true, wantElapsed: 5 * time.Minute},
		{name: "other cause never escalates", status: marketdata.TokenStatus{Issue: marketdata.TokenIssueUnreachable, Failures: 9, Since: now.Add(-time.Hour)}},
		{name: "rejected has no streak", status: marketdata.TokenStatus{Issue: marketdata.TokenIssueRejected, Code: 4001007}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			persistent, elapsed := kabu.SessionStatusOf(tt.status).Persistent(now)
			if persistent != tt.wantPersist || elapsed != tt.wantElapsed {
				t.Errorf("Persistent = %v, %v; want %v, %v", persistent, elapsed, tt.wantPersist, tt.wantElapsed)
			}
		})
	}
}
