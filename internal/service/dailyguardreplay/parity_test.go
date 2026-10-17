package dailyguardreplay_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/config/dailyguardsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/service/dailyguardreplay"
	"github.com/ousiassllc/pitha-trador/internal/service/risk"
)

// activityPortfolio serves a settable DailyActivity; everything else is the
// zero portfolio.
type activityPortfolio struct {
	risk.ZeroPortfolioProvider
	activity *risk.DailyActivity
}

func (p activityPortfolio) DailyActivity(context.Context) (risk.DailyActivity, error) {
	return *p.activity, nil
}

type step struct {
	pnl    float64
	trades int
}

// The replay's Guard must latch exactly when risk.Engine's daily guard does,
// step by step, for the same stored thresholds and the same P&L / trade
// sequence (the engine's decision function is unexported, so the engine is
// driven through CheckDailyGuard / DailyHalt).
func TestGuard_MatchesRiskEngine(t *testing.T) {
	tests := []struct {
		name string
		rows map[string]string
		seq  []step
	}{
		{"loss limit boundary", map[string]string{dailyguardsettings.KeyLossLimitJPY: "5000"},
			[]step{{0, 0}, {-4999, 1}, {-5000, 1}, {100, 2}}},
		{"profit lock boundary", map[string]string{dailyguardsettings.KeyProfitLockTriggerJPY: "2000", dailyguardsettings.KeyProfitLockGivebackPct: "50"},
			[]step{{1999, 1}, {1000, 1}, {2000, 2}, {1001, 2}, {1000, 3}, {5000, 3}}},
		{"profit lock needs both rows", map[string]string{dailyguardsettings.KeyProfitLockTriggerJPY: "2000"},
			[]step{{3000, 1}, {-100, 2}}},
		{"max trades boundary", map[string]string{dailyguardsettings.KeyMaxTrades: "3"},
			[]step{{0, 1}, {0, 2}, {0, 3}, {0, 4}}},
		{"order: loss limit before profit lock before trades",
			map[string]string{dailyguardsettings.KeyLossLimitJPY: "1000", dailyguardsettings.KeyProfitLockTriggerJPY: "500", dailyguardsettings.KeyProfitLockGivebackPct: "100", dailyguardsettings.KeyMaxTrades: "1"},
			[]step{{600, 0}, {-1000, 1}}},
		{"percent-derived thresholds", map[string]string{
			dailyguardsettings.KeyOperatingCapitalJPY: "500000", dailyguardsettings.KeyLossLimitPct: "1",
			dailyguardsettings.KeyProfitLockTriggerPct: "0.5", dailyguardsettings.KeyProfitLockGivebackPct: "40"},
			[]step{{2500, 1}, {1500, 1}, {1499, 2}, {-5000, 2}}},
		{"no rule enabled", map[string]string{}, []step{{-1e9, 99}, {1e9, 100}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "parity.db"))
			if err != nil {
				t.Fatalf("sqlitedb.Open: %v", err)
			}
			t.Cleanup(func() { _ = db.Close() })
			settings := system.NewRuntimeSettingsRepository(db)
			now := time.Date(2026, 10, 14, 10, 0, 0, 0, dailyguardreplay.TradingDayZone)
			for key, value := range tc.rows {
				if err := settings.Set(ctx, key, value, now); err != nil {
					t.Fatalf("set %s: %v", key, err)
				}
			}
			activity := &risk.DailyActivity{}
			engine := risk.NewEngine(risk.Config{
				Limits: config.RiskLimits{InitialCapital: 30_000_000}, KillSwitch: system.NewKillSwitchRepository(db), Settings: settings,
				Snapshots: market.NewSnapshotRepository(db), Portfolio: activityPortfolio{activity: activity},
				Now: func() time.Time { return now },
			})
			limits, err := dailyguardsettings.Load(ctx, settings)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			guard := dailyguardreplay.NewGuard(limits)

			for i, s := range tc.seq {
				*activity = risk.DailyActivity{PnLJPY: s.pnl, Trades: s.trades}
				if err := engine.CheckDailyGuard(ctx); err != nil {
					t.Fatalf("step %d: CheckDailyGuard: %v", i, err)
				}
				halt, err := engine.DailyHalt(ctx)
				if err != nil {
					t.Fatalf("step %d: DailyHalt: %v", i, err)
				}
				want := ""
				if halt != nil {
					want = halt.Reason
				}
				if got := guard.Update(s.pnl, s.trades); got != want {
					t.Fatalf("step %d (%+v): replay halt = %q, engine halt = %q", i, s, got, want)
				}
			}
		})
	}
}

// Replay-level invariants that mirror how the engine counts: only opens the
// guard let through count toward the trade limit, a refused trade never
// existed (its close is ignored), and the day's profit peak starts at 0.

func TestReplay_RefusedOpensAreNotKeptOpens(t *testing.T) {
	r := dailyguardreplay.Replay([]dailyguardreplay.Trade{
		trade(1, 9, 0, 9, 10, 100),
		trade(2, 9, 20, 9, 30, 100),
		trade(3, 9, 40, 9, 50, 100),
		trade(4, 10, 0, 10, 10, 100),
	}, dailyguardsettings.Limits{MaxTrades: 2})
	d := only(t, r)
	if d.TradesAll != 4 || d.TradesKept != 2 || d.TradesSkipped != 2 {
		t.Errorf("trades all/kept/skipped = %d/%d/%d, want 4/2/2 (refused opens do not count as kept)", d.TradesAll, d.TradesKept, d.TradesSkipped)
	}
	if d.HaltReason != domain.DailyHaltReasonMaxTrades || !d.HaltedAt.Equal(at(14, 9, 20)) {
		t.Errorf("halt = %q at %v, want max_trades at the second kept open (09:20)", d.HaltReason, d.HaltedAt)
	}
	if d.PnLWithoutJPY != 400 || d.PnLWithJPY != 200 || d.PnLSkippedJPY != 200 {
		t.Errorf("pnl without/with/skipped = %v/%v/%v, want 400/200/200", d.PnLWithoutJPY, d.PnLWithJPY, d.PnLSkippedJPY)
	}
}

func TestReplay_ClosingARefusedTradeIsIgnored(t *testing.T) {
	// Trade 1 reaches the loss limit; trade 2 is refused, so neither its
	// large gain nor trade 3's large loss (also refused) reaches the guarded
	// result or its drawdown.
	r := dailyguardreplay.Replay([]dailyguardreplay.Trade{
		trade(1, 9, 0, 9, 10, -1000),
		trade(2, 9, 20, 9, 40, 5000),
		trade(3, 9, 30, 9, 50, -9000),
	}, dailyguardsettings.Limits{LossLimitJPY: 1000})
	d := only(t, r)
	if d.PnLWithJPY != -1000 || d.MaxDrawdownWithJPY != 1000 {
		t.Errorf("with-guard pnl/drawdown = %v/%v, want -1000/1000 (refused closes ignored)", d.PnLWithJPY, d.MaxDrawdownWithJPY)
	}
	if d.PnLSkippedJPY != -4000 || d.PnLWithoutJPY != -5000 {
		t.Errorf("skipped/without pnl = %v/%v, want -4000/-5000", d.PnLSkippedJPY, d.PnLWithoutJPY)
	}
	if d.HaltReason != domain.DailyHaltReasonLossLimit {
		t.Errorf("halt = %q, want loss_limit", d.HaltReason)
	}
}

func TestReplay_ProfitPeakNeverBelowZero(t *testing.T) {
	limits := dailyguardsettings.Limits{ProfitLockTriggerJPY: 100, ProfitLockGivebackPct: 50}
	losingThenSmallGain := []dailyguardreplay.Trade{
		trade(1, 9, 0, 9, 10, -300),
		trade(2, 9, 20, 9, 30, 40),
	}
	if d := only(t, dailyguardreplay.Replay(losingThenSmallGain, limits)); d.HaltReason != "" || d.TradesKept != 2 {
		t.Errorf("a loss then a small gain halted (%q, kept %d); the profit lock must not trigger", d.HaltReason, d.TradesKept)
	}

	// The peak is the day's real high: +140 after the loss, the lock fires on
	// the fall to +60 (<= 140 - 50%), and the later entry is refused.
	r := dailyguardreplay.Replay(append(losingThenSmallGain,
		trade(3, 9, 40, 9, 50, 400),
		trade(4, 10, 0, 10, 10, -80),
		trade(5, 10, 20, 10, 30, 1),
	), limits)
	d := only(t, r)
	if d.HaltReason != domain.DailyHaltReasonProfitLock || !d.HaltedAt.Equal(at(14, 10, 10)) {
		t.Errorf("halt = %q at %v, want profit_lock at 10:10", d.HaltReason, d.HaltedAt)
	}
	if d.TradesKept != 4 || d.TradesSkipped != 1 || d.PnLWithJPY != 60 {
		t.Errorf("kept/skipped/with = %d/%d/%v, want 4/1/60", d.TradesKept, d.TradesSkipped, d.PnLWithJPY)
	}
}
