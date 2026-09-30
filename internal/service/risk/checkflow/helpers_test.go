// Package checkflow_test holds the Engine.Check tests of risk.Engine
// (FR-RISK-1). Engine's methods share unexported state and so cannot be
// split across packages; the tests live in their own directory to keep
// internal/service/risk under the linterly line budget (#247). They only
// use risk's exported API; the helpers below are this package's own
// (sibling test packages do not import each other).
package checkflow_test

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

// fakePortfolio is a configurable risk.PortfolioProvider for tests. Every
// field defaults to the zero value (matching risk.ZeroPortfolioProvider's
// behavior) unless a test overrides it.
type fakePortfolio struct {
	openPositionCount int
	totalExposurePct  float64
	symbolExposurePct float64
	dailyLossPct      float64
	consecutiveLosses int
	lastLossAt        time.Time
}

func (f fakePortfolio) OpenPositionCount(context.Context) (int, error) {
	return f.openPositionCount, nil
}
func (f fakePortfolio) TotalExposurePct(context.Context) (float64, error) {
	return f.totalExposurePct, nil
}
func (f fakePortfolio) SymbolExposurePct(context.Context, int64) (float64, error) {
	return f.symbolExposurePct, nil
}
func (f fakePortfolio) DailyLossPct(context.Context, time.Time) (float64, error) {
	return f.dailyLossPct, nil
}
func (f fakePortfolio) ConsecutiveLosses(context.Context, time.Time) (int, error) {
	return f.consecutiveLosses, nil
}
func (f fakePortfolio) LastLossAt(context.Context, time.Time) (time.Time, error) {
	return f.lastLossAt, nil
}

type fakeCloser struct {
	closed []string
}

func (f *fakeCloser) CloseAll(_ context.Context, reason string) error {
	f.closed = append(f.closed, reason)
	return nil
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

func newEngine(t *testing.T, limits config.RiskLimits, portfolio risk.PortfolioProvider, closer risk.PositionCloser, now func() time.Time) (*risk.Engine, *system.KillSwitchRepository) {
	t.Helper()
	db := newTestDB(t)
	killSwitch := system.NewKillSwitchRepository(db)
	if now == nil {
		now = time.Now
	}
	snapshots := market.NewSnapshotRepository(db)
	seedSnapshot(t, db, snapshots, 2000, 5)
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

// seedSnapshot creates instrument 1 (the id Check tests use) with one
// latest snapshot at price/spreadBps; Check fails closed without one.
// A negative spreadBps stores a NULL spread.
func seedSnapshot(t *testing.T, db *sql.DB, snapshots *market.SnapshotRepository, price, spreadBps float64) {
	t.Helper()
	inst, err := market.NewInstrumentRepository(db).Create(context.Background(), domain.Instrument{
		Symbol: "7203", Name: "Toyota", Market: "TSE Prime", IsActive: true,
	})
	if err != nil {
		t.Fatalf("create instrument: %v", err)
	}
	snap := domain.Snapshot{InstrumentID: inst.ID, Timestamp: time.Now(), Price: price, RawDataJSON: "{}"}
	if spreadBps >= 0 {
		snap.SpreadBps = &spreadBps
	}
	if _, err := snapshots.Insert(context.Background(), snap); err != nil {
		t.Fatalf("insert snapshot: %v", err)
	}
}
