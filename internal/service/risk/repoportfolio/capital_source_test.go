package repoportfolio_test

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/risk/repoportfolio"
)

// issue #804: with a capital source the baseline is read on every call, so a
// capital saved on the Settings screen applies at once; a source error or a
// non-positive capital fails the call (the Risk Engine then rejects).
func TestRepositoryPortfolioProvider_CapitalSourceIsReadOnEveryCall(t *testing.T) {
	_, positions, orders, instrumentID := newPortfolioTestFixtures(t)
	ctx := context.Background()
	mustOpenPosition(t, positions, orders, instrumentID, portfolioTestNow) // 100 x 2,100 = 210,000 yen

	capital := 1_000_000.0
	var sourceErr error
	provider := repoportfolio.New(positions, portfolioTestCapital, repoportfolio.WithCapitalSource(func(context.Context) (float64, error) { return capital, sourceErr }))

	if got, err := provider.TotalExposurePct(ctx); err != nil || math.Abs(got-21) > 1e-9 {
		t.Fatalf("TotalExposurePct at 1,000,000 = %v, %v, want 21", got, err)
	}
	capital = 500_000
	if got, err := provider.TotalExposurePct(ctx); err != nil || math.Abs(got-42) > 1e-9 {
		t.Fatalf("TotalExposurePct at 500,000 = %v, %v, want 42 (no restart)", got, err)
	}
	if got, err := provider.SymbolExposurePct(ctx, instrumentID); err != nil || math.Abs(got-42) > 1e-9 {
		t.Fatalf("SymbolExposurePct at 500,000 = %v, %v, want 42", got, err)
	}

	capital = 0
	if _, err := provider.TotalExposurePct(ctx); !errors.Is(err, repoportfolio.ErrNoInitialCapital) {
		t.Errorf("TotalExposurePct with capital 0 err = %v, want ErrNoInitialCapital", err)
	}
	sourceErr = errors.New("settings store down")
	if _, err := provider.DailyLossPct(ctx, time.Time{}); !errors.Is(err, sourceErr) {
		t.Errorf("DailyLossPct err = %v, want the source error", err)
	}
}
