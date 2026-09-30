// Package monitorflow_test holds the periodic-monitor, daily-loss-warning
// and Notifier tests of risk.Engine (FR-RISK-3〜7). Engine's methods share
// unexported state and so cannot be split across packages; the tests live
// in their own directory to keep internal/service/risk under the linterly
// line budget (#247). They only use risk's exported API; the helpers below
// are this package's own (sibling test packages do not import each other).
package monitorflow_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
)

func testLimits() config.RiskLimits {
	return config.RiskLimits{
		InitialCapital:                    30_000_000,
		MaxPositionPerSymbolPct:           2.0,
		MaxTotalExposurePct:               20.0,
		MaxDailyLossPct:                   1.0,
		MaxTradeLossPct:                   0.25,
		MaxOpenPositions:                  5,
		MaxSpreadBps:                      30,
		MaxConsecutiveLosses:              4,
		CooldownAfterLossMinutes:          5,
		ForceFlatBeforeMarketCloseMinutes: 10,
		// HeartbeatTimeoutMinutes left 0: Paper mode, FR-RISK-6 disabled.
	}
}

// fakePortfolio is a risk.PortfolioProvider reporting a fixed daily loss;
// every other reading is zero (matching risk.ZeroPortfolioProvider).
type fakePortfolio struct {
	dailyLossPct float64
}

func (fakePortfolio) OpenPositionCount(context.Context) (int, error)            { return 0, nil }
func (fakePortfolio) TotalExposurePct(context.Context) (float64, error)         { return 0, nil }
func (fakePortfolio) SymbolExposurePct(context.Context, int64) (float64, error) { return 0, nil }
func (f fakePortfolio) DailyLossPct(context.Context, time.Time) (float64, error) {
	return f.dailyLossPct, nil
}
func (fakePortfolio) ConsecutiveLosses(context.Context, time.Time) (int, error) { return 0, nil }
func (fakePortfolio) LastLossAt(context.Context, time.Time) (time.Time, error) {
	return time.Time{}, nil
}

type fakeCloser struct {
	closed []string
}

func (f *fakeCloser) CloseAll(_ context.Context, reason string) error {
	f.closed = append(f.closed, reason)
	return nil
}

// killSwitchTriggeredCall/dailyLossWarningCall record one fakeNotifier
// method invocation each, for tests asserting on exactly what a Notifier
// was told.
type killSwitchTriggeredCall struct {
	event         domain.KillSwitchEvent
	autoResumable bool
}

type dailyLossWarningCall struct {
	currentPct float64
	limitPct   float64
}

// fakeNotifier is a configurable risk.Notifier for tests: every method
// records its call and returns err (nil unless a test wants to exercise
// the "notifier failed, caller must not block on it" paths in state.go/
// autoresume.go/warning.go).
type fakeNotifier struct {
	err error

	triggered   []killSwitchTriggeredCall
	autoResumed []domain.KillSwitchEvent
	dailyLoss   []dailyLossWarningCall
}

func (f *fakeNotifier) KillSwitchTriggered(_ context.Context, ev domain.KillSwitchEvent, autoResumable bool) error {
	f.triggered = append(f.triggered, killSwitchTriggeredCall{event: ev, autoResumable: autoResumable})
	return f.err
}

func (f *fakeNotifier) KillSwitchAutoResumed(_ context.Context, ev domain.KillSwitchEvent) error {
	f.autoResumed = append(f.autoResumed, ev)
	return f.err
}

func (f *fakeNotifier) DailyLossWarning(_ context.Context, currentPct, limitPct float64) error {
	f.dailyLoss = append(f.dailyLoss, dailyLossWarningCall{currentPct: currentPct, limitPct: limitPct})
	return f.err
}

// newTestDB opens a fresh, fully migrated SQLite database in t's
// temporary directory and registers it to close when t completes.
func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "risk_test.db"))
	if err != nil {
		t.Fatalf("sqlitedb.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}
