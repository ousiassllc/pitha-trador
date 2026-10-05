package checkflow_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/service/risk"
)

// newEngineWithMarketReturn5m is newEngine whose only snapshot (instrument
// 1) carries marketReturn5m (a decimal ratio; nil leaves it unavailable).
func newEngineWithMarketReturn5m(t *testing.T, portfolio risk.PortfolioProvider, marketReturn5m *float64) *risk.Engine {
	t.Helper()
	db := newTestDB(t)
	inst, err := market.NewInstrumentRepository(db).Create(context.Background(), domain.Instrument{
		Symbol: "7203", Name: "Toyota", Market: "TSE Prime", IsActive: true,
	})
	if err != nil {
		t.Fatalf("create instrument: %v", err)
	}
	spread := 5.0
	snapshots := market.NewSnapshotRepository(db)
	_, err = snapshots.Insert(context.Background(), domain.Snapshot{
		InstrumentID: inst.ID, Timestamp: time.Now(), Price: 2000, SpreadBps: &spread, RawDataJSON: "{}",
		Feature: domain.Feature{MarketReturn5m: marketReturn5m},
	})
	if err != nil {
		t.Fatalf("insert snapshot: %v", err)
	}
	return risk.NewEngine(risk.Config{
		Limits:     testLimits(),
		KillSwitch: system.NewKillSwitchRepository(db),
		Settings:   system.NewRuntimeSettingsRepository(db),
		Snapshots:  snapshots,
		Portfolio:  portfolio,
	})
}

func TestEngine_Check_MaxSameDirectionPositions(t *testing.T) {
	limits := testLimits()
	atCap := limits.MaxSameDirectionPositions
	tests := []struct {
		name      string
		portfolio fakePortfolio
		direction string
		wantPass  bool
	}{
		{"LONG at cap is rejected", fakePortfolio{longPositions: atCap}, domain.JevDirectionLong, false},
		{"SHORT at cap is rejected", fakePortfolio{shortPositions: atCap}, domain.JevDirectionShort, false},
		{"LONG below cap passes", fakePortfolio{longPositions: atCap - 1}, domain.JevDirectionLong, true},
		{"opposite direction is not counted", fakePortfolio{longPositions: atCap}, domain.JevDirectionShort, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, _ := newEngine(t, limits, tt.portfolio, nil, nil)

			passed, reason := e.Check(context.Background(), 1, tt.direction)

			if passed != tt.wantPass {
				t.Fatalf("Check = (%v, %q), want passed=%v", passed, reason, tt.wantPass)
			}
			if !tt.wantPass && !strings.HasPrefix(reason, risk.ReasonMaxSameDirection) {
				t.Fatalf("reason = %q, want prefix %q", reason, risk.ReasonMaxSameDirection)
			}
		})
	}
}

func TestEngine_Check_MarketAdverseToHeldDirection(t *testing.T) {
	ptr := func(v float64) *float64 { return &v }
	tests := []struct {
		name      string
		portfolio fakePortfolio
		direction string
		market5m  *float64
		wantPass  bool
	}{
		{"LONG held, market down past threshold", fakePortfolio{longPositions: 1}, domain.JevDirectionLong, ptr(-0.003), false},
		{"LONG held, market down exactly at threshold", fakePortfolio{longPositions: 1}, domain.JevDirectionLong, ptr(-0.002), false},
		{"LONG held, market down within threshold", fakePortfolio{longPositions: 1}, domain.JevDirectionLong, ptr(-0.001), true},
		{"LONG held, market up", fakePortfolio{longPositions: 1}, domain.JevDirectionLong, ptr(0.005), true},
		{"SHORT held, market up past threshold", fakePortfolio{shortPositions: 1}, domain.JevDirectionShort, ptr(0.003), false},
		{"SHORT held, market down", fakePortfolio{shortPositions: 1}, domain.JevDirectionShort, ptr(-0.005), true},
		{"nothing held in that direction", fakePortfolio{shortPositions: 1}, domain.JevDirectionLong, ptr(-0.005), true},
		{"market return unavailable", fakePortfolio{longPositions: 1}, domain.JevDirectionLong, nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEngineWithMarketReturn5m(t, tt.portfolio, tt.market5m)

			passed, reason := e.Check(context.Background(), 1, tt.direction)

			if passed != tt.wantPass {
				t.Fatalf("Check = (%v, %q), want passed=%v", passed, reason, tt.wantPass)
			}
			if !tt.wantPass && !strings.HasPrefix(reason, risk.ReasonMarketAdverse) {
				t.Fatalf("reason = %q, want prefix %q", reason, risk.ReasonMarketAdverse)
			}
		})
	}
}
