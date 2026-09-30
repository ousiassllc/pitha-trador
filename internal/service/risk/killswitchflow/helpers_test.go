// Package killswitchflow_test holds the Resume / AutoResume / Kill flow
// tests of risk.Engine (FR-RISK-2/4/5/6/7). They live in their own
// directory to keep internal/service/risk under the linterly line budget;
// the helpers below are this package's own (sibling test packages such as
// checkflow and monitorflow do not import each other).
package killswitchflow_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
	"github.com/ousiassllc/pitha-trador/internal/service/risk"
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

// fakeTrade is a closed trade fakePortfolio derives loss state from,
// honouring the manual-resume baseline (since) like the real
// repoportfolio.Provider. lossPct is the loss as % of equity (>0 = loss).
type fakeTrade struct {
	closedAt time.Time
	lossPct  float64
}

// fakePortfolio is a risk.PortfolioProvider over trades (oldest first);
// with no trades and openPositions 0 it behaves like
// risk.ZeroPortfolioProvider.
type fakePortfolio struct {
	trades        []fakeTrade
	openPositions int
}

func (f fakePortfolio) OpenPositionCount(context.Context) (int, error)          { return f.openPositions, nil }
func (fakePortfolio) TotalExposurePct(context.Context) (float64, error)         { return 0, nil }
func (fakePortfolio) SymbolExposurePct(context.Context, int64) (float64, error) { return 0, nil }

func (f fakePortfolio) DailyLossPct(_ context.Context, since time.Time) (float64, error) {
	var sum float64
	for _, t := range f.trades {
		if t.closedAt.After(since) {
			sum += t.lossPct
		}
	}
	return sum, nil
}

func (f fakePortfolio) ConsecutiveLosses(_ context.Context, since time.Time) (int, error) {
	n := 0
	for i := len(f.trades) - 1; i >= 0; i-- {
		t := f.trades[i]
		if !t.closedAt.After(since) || t.lossPct <= 0 {
			break
		}
		n++
	}
	return n, nil
}

func (f fakePortfolio) LastLossAt(_ context.Context, since time.Time) (time.Time, error) {
	var last time.Time
	for _, t := range f.trades {
		if t.lossPct > 0 && t.closedAt.After(since) && t.closedAt.After(last) {
			last = t.closedAt
		}
	}
	return last, nil
}

type fakeCloser struct{ closed []string }

func (f *fakeCloser) CloseAll(_ context.Context, reason string) error {
	f.closed = append(f.closed, reason)
	return nil
}

type fakeHealth struct{ healthy bool }

func (f fakeHealth) Healthy(context.Context) (bool, error) { return f.healthy, nil }

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "flow_test.db"))
	if err != nil {
		t.Fatalf("sqlitedb.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func jstTime(y int, m time.Month, d, hh, mm int) time.Time {
	return time.Date(y, m, d, hh, mm, 0, 0, marketcalendar.JST)
}

// newEngine builds an Engine on a fresh migrated DB. Check needs a latest
// snapshot for instrument 1, so one is seeded.
func newEngine(t *testing.T, limits config.RiskLimits, portfolio risk.PortfolioProvider, closer risk.PositionCloser, now func() time.Time) (*risk.Engine, *system.KillSwitchRepository) {
	t.Helper()
	db := newTestDB(t)
	killSwitch := system.NewKillSwitchRepository(db)
	if now == nil {
		now = time.Now
	}
	snapshots := market.NewSnapshotRepository(db)
	inst, err := market.NewInstrumentRepository(db).Create(context.Background(), domain.Instrument{
		Symbol: "7203", Name: "Toyota", Market: "TSE Prime", IsActive: true,
	})
	if err != nil {
		t.Fatalf("create instrument: %v", err)
	}
	spread := 5.0
	if _, err := snapshots.Insert(context.Background(), domain.Snapshot{
		InstrumentID: inst.ID, Timestamp: time.Now(), Price: 2000, SpreadBps: &spread, RawDataJSON: "{}",
	}); err != nil {
		t.Fatalf("insert snapshot: %v", err)
	}
	e := risk.NewEngine(risk.Config{
		Limits:     limits,
		KillSwitch: killSwitch,
		Settings:   system.NewRuntimeSettingsRepository(db),
		Snapshots:  snapshots,
		Portfolio:  portfolio,
		Closer:     closer,
		Now:        now,
	})
	return e, killSwitch
}

// newSessionEngine is an Engine with a TSE Calendar, Live heartbeat timeout
// (120 min) and a caller-controlled clock.
func newSessionEngine(t *testing.T, clock *time.Time, md risk.HealthChecker) (*risk.Engine, *system.KillSwitchRepository) {
	t.Helper()
	db := newTestDB(t)
	killSwitch := system.NewKillSwitchRepository(db)
	limits := testLimits()
	limits.HeartbeatTimeoutMinutes = 120
	e := risk.NewEngine(risk.Config{
		Limits:           limits,
		KillSwitch:       killSwitch,
		Settings:         system.NewRuntimeSettingsRepository(db),
		Portfolio:        risk.ZeroPortfolioProvider{},
		MarketDataHealth: md,
		Calendar:         marketcalendar.TSE,
		Now:              func() time.Time { return *clock },
	})
	return e, killSwitch
}

func unresolvedReasons(t *testing.T, ks *system.KillSwitchRepository) []string {
	t.Helper()
	events, err := ks.ListUnresolved(context.Background())
	if err != nil {
		t.Fatalf("ListUnresolved: %v", err)
	}
	var reasons []string
	for _, ev := range events {
		reasons = append(reasons, ev.Reason)
	}
	return reasons
}
