package insight_test

import (
	"math"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/insight"
)

func closedPosition(pnl float64, opened, closed time.Time) domain.Position {
	return domain.Position{
		Symbol: "7203", Side: domain.PositionSideLong, Quantity: 100, EntryPrice: 1000,
		RealizedPnL: &pnl, OpenedAt: opened, ClosedAt: &closed,
	}
}

func approx(t *testing.T, name string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-3 {
		t.Fatalf("%s = %v, want %v", name, got, want)
	}
}

func TestAggregatePerformance_ComputesStatisticsAndJSTDailyBoundary(t *testing.T) {
	// now = 2026-09-28 12:00 JST; "today" starts at 2026-09-27T15:00Z.
	now := time.Date(2026, 9, 28, 3, 0, 0, 0, time.UTC)
	c := closedPosition(2000, time.Date(2026, 9, 27, 14, 0, 0, 0, time.UTC), time.Date(2026, 9, 27, 14, 30, 0, 0, time.UTC))  // 23:30 JST, yesterday
	a := closedPosition(1000, time.Date(2026, 9, 27, 15, 20, 0, 0, time.UTC), time.Date(2026, 9, 27, 15, 30, 0, 0, time.UTC)) // 00:30 JST, today
	b := closedPosition(-500, time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC), time.Date(2026, 9, 28, 0, 20, 0, 0, time.UTC))
	open := domain.Position{Symbol: "7203", Quantity: 100, EntryPrice: 1000, OpenedAt: now}

	got := insight.Aggregate([]domain.Position{a, open, b, c}, 7, now)

	if got.TradeCount != 3 {
		t.Fatalf("TradeCount = %d, want 3 (open position ignored)", got.TradeCount)
	}
	approx(t, "TotalPnL", got.TotalPnL, 2500)
	approx(t, "DailyPnL", got.DailyPnL, 500)
	approx(t, "WinRate", got.WinRate, 2.0/3)
	if got.ProfitFactor == nil {
		t.Fatal("ProfitFactor = nil, want 6")
	}
	approx(t, "ProfitFactor", *got.ProfitFactor, 6)
	approx(t, "Expectancy", got.Expectancy, 2.5/3)
	approx(t, "MaxDrawdownPct", got.MaxDrawdownPct, 0.5)
	approx(t, "AverageHoldTimeMinutes", got.AverageHoldTimeMinutes, 20)
	if got.SharpeRef == nil || got.SortinoRef == nil {
		t.Fatalf("SharpeRef/SortinoRef = %v/%v, want both set", got.SharpeRef, got.SortinoRef)
	}
	approx(t, "SharpeRef", *got.SharpeRef, 0.6623)
	approx(t, "SortinoRef", *got.SortinoRef, 2.8868)
	if got.SignalCount != 7 {
		t.Fatalf("SignalCount = %d, want 7", got.SignalCount)
	}
}

func TestAggregatePerformance_UndefinedRatiosAreNil(t *testing.T) {
	now := time.Date(2026, 9, 28, 3, 0, 0, 0, time.UTC)

	empty := insight.Aggregate(nil, 0, now)
	if empty.TradeCount != 0 || empty.ProfitFactor != nil || empty.SharpeRef != nil || empty.SortinoRef != nil {
		t.Fatalf("empty = %+v, want zero trades and nil ratios", empty)
	}

	winOnly := insight.Aggregate([]domain.Position{
		closedPosition(1000, now.Add(-time.Hour), now.Add(-30*time.Minute)),
	}, 0, now)
	if winOnly.ProfitFactor != nil || winOnly.SharpeRef != nil || winOnly.SortinoRef != nil {
		t.Fatalf("single winning trade = %+v, want nil ProfitFactor/SharpeRef/SortinoRef", winOnly)
	}
	approx(t, "WinRate", winOnly.WinRate, 1)
}
