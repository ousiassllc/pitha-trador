// Package healthalertwire builds what watches the trading hours from the
// composition root's pieces: the market-data alert detector (issue #802) and
// the sleep guard (issue #863), which both follow the 08:45 check window and
// run together. It lives in its own package so internal/bootstrap stays within
// its size limit.
package healthalertwire

import (
	"context"
	"database/sql"
	"sync"

	"github.com/ousiassllc/pitha-trador/internal/config/sleepguardsettings"
	"github.com/ousiassllc/pitha-trador/internal/repository/jobqueue"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/service/broker"
	"github.com/ousiassllc/pitha-trador/internal/service/healthalert"
	"github.com/ousiassllc/pitha-trador/internal/service/marketdata/kabu"
	"github.com/ousiassllc/pitha-trador/internal/service/sleepguard"
)

// Watch is the Detector (Active, Check: the banner and the MCP read its
// alerts) together with the sleep guard. Run runs both.
type Watch struct {
	*healthalert.Detector
	guard *sleepguard.Guard
}

// Run runs the Detector and the sleep guard until ctx is done; the guard gives
// the PC back to the OS power settings before Run returns.
func (w *Watch) Run(ctx context.Context) {
	var wg sync.WaitGroup
	wg.Go(func() { w.Detector.Run(ctx) })
	wg.Go(func() { w.guard.Run(ctx) })
	wg.Wait()
}

// New returns the Watch: its Detector persists its alerts in health_alerts, so
// the header banner (Detector.Active) and the Paper results MCP, a separate
// read-only process, show the same state; its sleep guard reads the Settings
// switch from settings at every check. The caller runs Watch.Run.
func New(db *sql.DB, jobs *jobqueue.JobRepository, b broker.Broker, settings sleepguardsettings.Store) *Watch {
	return &Watch{
		Detector: healthalert.New(healthalert.Config{
			Store: system.NewHealthAlertRepository(db),
			Jobs:  jobs,
			Token: func() healthalert.TokenState { return kabuTokenState(b) },
		}),
		guard: sleepguard.New(sleepguard.Config{
			Settings: sleepguardsettings.Source{Store: settings},
			Keeper:   sleepguard.NewKeeper(),
		}),
	}
}

// kabuTokenState is the kabu token's not-logged-in streak (4001007) or its
// connection-refused streak (kabuステーション not running; issue #863), or none
// while kabu is logged in, fails for another reason, or is not the selected
// broker (the alerts are kabu's).
func kabuTokenState(b broker.Broker) healthalert.TokenState {
	if b.Capabilities().Name != kabu.Name {
		return healthalert.TokenState{}
	}
	switch status := b.Status(); status.Issue {
	case broker.SessionIssueNotLoggedIn:
		return healthalert.TokenState{NotLoggedInSince: status.Since}
	case broker.SessionIssueUnreachable:
		return healthalert.TokenState{UnreachableSince: status.Since}
	}
	return healthalert.TokenState{}
}
