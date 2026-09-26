package risk_test

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/repository"
	"github.com/ousiassllc/pitha-trador/internal/service/risk"
)

func testLimits() config.RiskLimits {
	return config.RiskLimits{
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
func (f fakePortfolio) DailyLossPct(context.Context) (float64, error) { return f.dailyLossPct, nil }
func (f fakePortfolio) ConsecutiveLosses(context.Context) (int, error) {
	return f.consecutiveLosses, nil
}
func (f fakePortfolio) LastLossAt(context.Context) (time.Time, error) { return f.lastLossAt, nil }

type fakeCloser struct {
	closed []string
}

func (f *fakeCloser) CloseAll(_ context.Context, reason string) error {
	f.closed = append(f.closed, reason)
	return nil
}

type fakeHealth struct {
	healthy bool
}

func (f fakeHealth) Healthy(context.Context) (bool, error) { return f.healthy, nil }

func newEngine(t *testing.T, limits config.RiskLimits, portfolio risk.PortfolioProvider, closer risk.PositionCloser, now func() time.Time) (*risk.Engine, *repository.KillSwitchRepository) {
	t.Helper()
	db := newTestDB(t)
	killSwitch := repository.NewKillSwitchRepository(db)
	if now == nil {
		now = time.Now
	}
	e := risk.NewEngine(risk.Config{
		Limits:     limits,
		KillSwitch: killSwitch,
		Settings:   repository.NewRuntimeSettingsRepository(db),
		Snapshots:  repository.NewSnapshotRepository(db),
		Portfolio:  portfolio,
		Closer:     closer,
		Now:        now,
	})
	return e, killSwitch
}
