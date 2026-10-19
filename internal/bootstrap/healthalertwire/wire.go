// Package healthalertwire builds the trading-hours market-data alert detector
// (issue #802) from the composition root's pieces. It lives in its own package
// so internal/bootstrap stays within its size limit.
package healthalertwire

import (
	"database/sql"

	"github.com/ousiassllc/pitha-trador/internal/repository/jobqueue"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/service/broker"
	"github.com/ousiassllc/pitha-trador/internal/service/healthalert"
	"github.com/ousiassllc/pitha-trador/internal/service/marketdata/kabu"
)

// New returns the Detector: it persists its alerts in health_alerts, so the
// header banner (Detector.Active) and the Paper results MCP, a separate
// read-only process, show the same state. The caller runs Detector.Run.
func New(db *sql.DB, jobs *jobqueue.JobRepository, b broker.Broker) *healthalert.Detector {
	return healthalert.New(healthalert.Config{
		Store: system.NewHealthAlertRepository(db),
		Jobs:  jobs,
		Token: func() healthalert.TokenState { return kabuTokenState(b) },
	})
}

// kabuTokenState is the kabu token's not-logged-in streak (4001007), or none
// while kabu is logged in, fails for another reason, or is not the selected
// broker (the alert is kabu's).
func kabuTokenState(b broker.Broker) healthalert.TokenState {
	if b.Capabilities().Name != kabu.Name {
		return healthalert.TokenState{}
	}
	if status := b.Status(); status.Issue == broker.SessionIssueNotLoggedIn {
		return healthalert.TokenState{NotLoggedInSince: status.Since}
	}
	return healthalert.TokenState{}
}
