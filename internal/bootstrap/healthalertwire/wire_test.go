package healthalertwire

import (
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/broker"
	"github.com/ousiassllc/pitha-trador/internal/service/healthalert"
	"github.com/ousiassllc/pitha-trador/internal/service/marketdata/kabu"
)

// fakeBroker is the kabu broker with a given session status.
type fakeBroker struct {
	broker.Broker
	name   string
	status broker.SessionStatus
}

func (f fakeBroker) Capabilities() broker.Capabilities { return broker.Capabilities{Name: f.name} }
func (f fakeBroker) Status() broker.SessionStatus      { return f.status }

// issue #863: the unreachable streak's start is passed on, next to the
// not-logged-in one; other issues and other brokers pass nothing.
func TestKabuTokenState(t *testing.T) {
	since := time.Date(2026, 10, 20, 8, 50, 0, 0, time.UTC)
	tests := []struct {
		name   string
		broker string
		issue  broker.SessionIssue
		want   healthalert.TokenState
	}{
		{"not logged in", kabu.Name, broker.SessionIssueNotLoggedIn, healthalert.TokenState{NotLoggedInSince: since}},
		{"connection refused", kabu.Name, broker.SessionIssueUnreachable, healthalert.TokenState{UnreachableSince: since}},
		{"another failure", kabu.Name, broker.SessionIssueAPIDisabled, healthalert.TokenState{}},
		{"logged in", kabu.Name, broker.SessionIssueNone, healthalert.TokenState{}},
		{"not the kabu broker", "tachibana", broker.SessionIssueUnreachable, healthalert.TokenState{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := kabuTokenState(fakeBroker{name: tt.broker, status: broker.SessionStatus{Issue: tt.issue, Since: since}})
			if got != tt.want {
				t.Errorf("kabuTokenState = %+v, want %+v", got, tt.want)
			}
		})
	}
}
