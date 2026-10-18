package simrisk_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest/simrisk"
	"github.com/ousiassllc/pitha-trador/internal/service/risk"
)

var jst = time.FixedZone("JST", 9*60*60)

func limits() config.RiskLimits {
	return config.RiskLimits{
		InitialCapital: 10_000_000, MaxPositionPerSymbolPct: 10, MaxTotalExposurePct: 50,
		MaxDailyLossPct: 2, MaxTradeLossPct: 0.5, MaxOpenPositions: 2, MaxSameDirectionPositions: 2,
		MarketAdverseReturn5mPct: 0.3, MaxSpreadBps: 30, MaxConsecutiveLosses: 3, CooldownAfterLossMinutes: 5,
	}
}

func at(h, m int) time.Time { return time.Date(2026, 9, 29, h, m, 0, 0, jst) }

func bar(price float64) domain.Snapshot {
	spread := 10.0
	return domain.Snapshot{Price: price, SpreadBps: &spread}
}

// check asks the portfolio whether instrument may enter side at now.
func check(p *simrisk.Portfolio, now time.Time, instrument int64, side string) (bool, string) {
	p.Advance(now)
	p.Candidate(bar(2000))
	return p.Check(context.Background(), instrument, side)
}

func wantRejected(t *testing.T, p *simrisk.Portfolio, now time.Time, instrument int64, side, reason string) {
	t.Helper()
	ok, got := check(p, now, instrument, side)
	if ok || !strings.HasPrefix(got, reason) {
		t.Fatalf("Check = (%v, %q), want a rejection starting with %q", ok, got, reason)
	}
}

func TestPortfolio_AllowsAnEntryWithinEveryLimit(t *testing.T) {
	p := simrisk.New(simrisk.Config{Limits: limits()})
	if ok, reason := check(p, at(9, 30), 1, domain.JevDirectionLong); !ok {
		t.Fatalf("Check rejected a clean entry: %q", reason)
	}
}

func TestPortfolio_MaxOpenPositions(t *testing.T) {
	p := simrisk.New(simrisk.Config{Limits: limits()})
	p.Open(1, domain.PositionSideLong, 100, 2000)
	p.Open(2, domain.PositionSideShort, 100, 2000)
	wantRejected(t, p, at(9, 30), 3, domain.JevDirectionLong, risk.ReasonMaxOpenPositions)
}

func TestPortfolio_MaxSameDirectionPositions(t *testing.T) {
	l := limits()
	l.MaxOpenPositions = 5
	p := simrisk.New(simrisk.Config{Limits: l})
	p.Open(1, domain.PositionSideLong, 100, 2000)
	p.Open(2, domain.PositionSideLong, 100, 2000)
	wantRejected(t, p, at(9, 30), 3, domain.JevDirectionLong, risk.ReasonMaxSameDirection)
	if ok, reason := check(p, at(9, 30), 3, domain.JevDirectionShort); !ok {
		t.Fatalf("the opposite direction was rejected: %q", reason)
	}
}

func TestPortfolio_MarketAdverseOnlyWhenStackingTheSameDirection(t *testing.T) {
	p := simrisk.New(simrisk.Config{Limits: limits()})
	adverse := bar(2000)
	marketDown := -0.005 // -0.5% against a LONG
	adverse.Feature.MarketReturn5m = &marketDown
	p.Advance(at(9, 30))
	p.Candidate(adverse)
	if ok, reason := p.Check(context.Background(), 1, domain.JevDirectionLong); !ok {
		t.Fatalf("a first LONG was rejected as market adverse: %q", reason)
	}
	p.Open(2, domain.PositionSideLong, 100, 2000)
	p.Candidate(adverse)
	ok, reason := p.Check(context.Background(), 1, domain.JevDirectionLong)
	if ok || !strings.HasPrefix(reason, risk.ReasonMarketAdverse) {
		t.Fatalf("Check = (%v, %q), want market_adverse for a second LONG into a falling market", ok, reason)
	}
}

func TestPortfolio_CooldownAfterLossBlocksEntriesUntilItElapses(t *testing.T) {
	p := simrisk.New(simrisk.Config{Limits: limits()})
	p.Advance(at(9, 30))
	p.Open(1, domain.PositionSideLong, 100, 2000)
	p.Close(1, at(9, 30), -1000)
	wantRejected(t, p, at(9, 34), 2, domain.JevDirectionLong, risk.ReasonCooldownAfterLoss)
	if ok, reason := check(p, at(9, 35), 2, domain.JevDirectionLong); !ok {
		t.Fatalf("still in cooldown after 5 minutes: %q", reason)
	}
}

func TestPortfolio_DailyLossLimitHaltsTheRestOfTheDayAndLiftsNextDay(t *testing.T) {
	p := simrisk.New(simrisk.Config{Limits: limits()})
	p.Advance(at(9, 30))
	p.Open(1, domain.PositionSideLong, 100, 2000)
	p.Close(1, at(9, 31), -250_000) // -2.5% of 10M: past the 2% limit
	wantRejected(t, p, at(10, 30), 2, domain.JevDirectionLong, risk.ReasonMaxDailyLossPct)
	// A recovery never lifts it within the day (live's Kill Switch is latched).
	p.Open(2, domain.PositionSideLong, 100, 2000)
	p.Close(2, at(10, 31), 900_000)
	wantRejected(t, p, at(14, 0), 3, domain.JevDirectionLong, risk.ReasonMaxDailyLossPct)

	next := at(9, 30).AddDate(0, 0, 1)
	if ok, reason := check(p, next, 3, domain.JevDirectionLong); !ok {
		t.Fatalf("the halt was not lifted by the assumed next-day resume: %q", reason)
	}
}

func TestPortfolio_DailyLossCountsUnrealizedLoss(t *testing.T) {
	p := simrisk.New(simrisk.Config{Limits: limits()})
	p.Advance(at(9, 30))
	p.Open(1, domain.PositionSideLong, 10_000, 2000)
	p.Observe(1, 1970) // -30 * 10_000 = -300_000 = -3%
	wantRejected(t, p, at(9, 31), 2, domain.JevDirectionShort, risk.ReasonMaxDailyLossPct)
}

func TestPortfolio_ConsecutiveLossesHaltAndAWinnerResetsTheStreak(t *testing.T) {
	l := limits()
	l.MaxDailyLossPct = 50
	l.CooldownAfterLossMinutes = 0
	p := simrisk.New(simrisk.Config{Limits: l})
	lose := func(id int64, h, m int, pnl float64) {
		p.Advance(at(h, m))
		p.Open(id, domain.PositionSideLong, 100, 2000)
		p.Close(id, at(h, m), pnl)
	}
	lose(1, 9, 30, -100)
	lose(2, 9, 40, -100)
	lose(3, 9, 50, 100) // a winner ends the streak
	lose(4, 10, 0, -100)
	lose(5, 10, 10, -100)
	if ok, reason := check(p, at(10, 20), 6, domain.JevDirectionLong); !ok {
		t.Fatalf("two losses after a winner halted entries: %q", reason)
	}
	lose(6, 10, 20, -100)
	wantRejected(t, p, at(10, 30), 7, domain.JevDirectionLong, risk.ReasonMaxConsecutiveLosses)
	if ok, reason := check(p, at(9, 30).AddDate(0, 0, 1), 7, domain.JevDirectionLong); !ok {
		t.Fatalf("the streak halt was not lifted by the assumed next-day resume: %q", reason)
	}
}

func TestPortfolio_SpreadAndSizing(t *testing.T) {
	p := simrisk.New(simrisk.Config{Limits: limits()})
	wide := bar(2000)
	spread := 80.0
	wide.SpreadBps = &spread
	p.Advance(at(9, 30))
	p.Candidate(wide)
	if ok, reason := p.Check(context.Background(), 1, domain.JevDirectionLong); ok || !strings.HasPrefix(reason, risk.ReasonMaxSpreadBps) {
		t.Fatalf("Check = (%v, %q), want max_spread_bps", ok, reason)
	}

	// One lot (100 shares at a very high price) breaks max_trade_loss_pct: no size fits.
	p.Candidate(bar(5_000_000))
	if ok, reason := p.Check(context.Background(), 1, domain.JevDirectionLong); ok || reason == "" {
		t.Fatalf("Check = (%v, %q), want a sizing rejection", ok, reason)
	}
	if got := p.Size(5_000_000); got != 0 {
		t.Fatalf("Size(5,000,000) = %d, want 0", got)
	}
	// 10M * 0.5% / (2000 * 0.6%) = 4166 shares -> 4100; the per-symbol cap (10% = 500 shares) binds first.
	if got := p.Size(2000); got != 500 {
		t.Fatalf("Size(2000) = %d, want 500 (max_position_per_symbol_pct binds)", got)
	}
}
