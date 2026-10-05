package checkflow_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/risk"
)

var errPortfolioDown = errors.New("database is locked")

// erroring wraps a healthy portfolio and fails exactly one method, so each
// portfolio read is proven to fail closed on its own.
type erroring struct {
	fakePortfolio
	failing string
}

func (e erroring) fail(name string) error {
	if e.failing == name {
		return errPortfolioDown
	}
	return nil
}
func (e erroring) OpenPositionCount(ctx context.Context) (int, error) {
	return 0, e.fail("OpenPositionCount")
}
func (e erroring) OpenPositionCountBySide(ctx context.Context, _ string) (int, error) {
	return 0, e.fail("OpenPositionCountBySide")
}
func (e erroring) TotalExposurePct(ctx context.Context) (float64, error) {
	return 0, e.fail("TotalExposurePct")
}
func (e erroring) SymbolExposurePct(ctx context.Context, _ int64) (float64, error) {
	return 0, e.fail("SymbolExposurePct")
}
func (e erroring) DailyLossPct(ctx context.Context, _ time.Time) (float64, error) {
	return 0, e.fail("DailyLossPct")
}
func (e erroring) ConsecutiveLosses(ctx context.Context, _ time.Time) (int, error) {
	return 0, e.fail("ConsecutiveLosses")
}
func (e erroring) LastLossAt(ctx context.Context, _ time.Time) (time.Time, error) {
	return time.Time{}, e.fail("LastLossAt")
}

func TestEngine_Check_FailsClosedWhenAnyPortfolioReadErrors(t *testing.T) {
	for _, method := range []string{
		"OpenPositionCount", "OpenPositionCountBySide", "TotalExposurePct", "SymbolExposurePct",
		"DailyLossPct", "ConsecutiveLosses", "LastLossAt",
	} {
		t.Run(method, func(t *testing.T) {
			e, _ := newEngine(t, testLimits(), erroring{failing: method}, nil, nil)

			passed, reason := e.Check(context.Background(), 1, domain.JevDirectionLong)

			if passed || !strings.HasPrefix(reason, risk.ReasonRiskEngineError) {
				t.Fatalf("Check = (%v, %q), want (false, prefix %q)", passed, reason, risk.ReasonRiskEngineError)
			}
		})
	}
}
