package checkflow_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/dailyguardsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/service/risk"
)

// The daily entry-halt rules (issue #753, FR-RISK-8).

// haltNotifier records DailyEntryHalted; every other alert is a no-op.
type haltNotifier struct {
	risk.NoopNotifier
	halts []domain.DailyEntryHalt
	err   error
}

func (n *haltNotifier) DailyEntryHalted(_ context.Context, halt domain.DailyEntryHalt) error {
	n.halts = append(n.halts, halt)
	return n.err
}

// guardFixture is an Engine over a mutable portfolio reading and clock, with
// the daily-guard thresholds written through the same runtime_settings store
// the Settings screen uses.
type guardFixture struct {
	t        *testing.T
	engine   *risk.Engine
	killSw   *system.KillSwitchRepository
	settings *system.RuntimeSettingsRepository
	notifier *haltNotifier
	activity *risk.DailyActivity
	now      *time.Time
}

// jst is a time on 2026-10-14 (Wed) at the given JST clock.
func jst(hour, minute int) time.Time {
	return time.Date(2026, 10, 14, hour, minute, 0, 0, time.FixedZone("JST", 9*60*60))
}

func newGuardFixture(t *testing.T, thresholds map[string]string) *guardFixture {
	t.Helper()
	db := newTestDB(t)
	snapshots := market.NewSnapshotRepository(db)
	seedSnapshot(t, db, snapshots, 2000, 5)
	f := &guardFixture{
		t: t, killSw: system.NewKillSwitchRepository(db), settings: system.NewRuntimeSettingsRepository(db),
		notifier: &haltNotifier{}, activity: &risk.DailyActivity{}, now: new(time.Time),
	}
	*f.now = jst(10, 0)
	f.engine = risk.NewEngine(risk.Config{
		Limits: testLimits(), KillSwitch: f.killSw, Settings: f.settings, Snapshots: snapshots,
		Portfolio: guardPortfolio{activity: f.activity}, Notifier: f.notifier,
		Now: func() time.Time { return *f.now },
	})
	for key, value := range thresholds {
		f.set(key, value)
	}
	return f
}

func (f *guardFixture) set(key, value string) {
	f.t.Helper()
	if err := f.settings.Set(context.Background(), key, value, *f.now); err != nil {
		f.t.Fatalf("set %s: %v", key, err)
	}
}

// check runs Check with the day's P&L and trade count set to pnl/trades.
func (f *guardFixture) check(pnl float64, trades int) (bool, string) {
	f.t.Helper()
	*f.activity = risk.DailyActivity{PnLJPY: pnl, Trades: trades}
	return f.engine.Check(context.Background(), 1, domain.JevDirectionLong)
}

func (f *guardFixture) wantPass(pnl float64, trades int) {
	f.t.Helper()
	if passed, reason := f.check(pnl, trades); !passed {
		f.t.Fatalf("Check(pnl=%v, trades=%d) rejected: %s, want pass", pnl, trades, reason)
	}
}

func (f *guardFixture) wantHalt(pnl float64, trades int, wantReason string) {
	f.t.Helper()
	passed, reason := f.check(pnl, trades)
	if passed || !strings.HasPrefix(reason, wantReason+": ") {
		f.t.Fatalf("Check(pnl=%v, trades=%d) = (%v, %q), want rejection starting %q", pnl, trades, passed, reason, wantReason)
	}
}

// guardPortfolio is a ZeroPortfolioProvider whose DailyActivity is
// controlled by the test.
type guardPortfolio struct {
	risk.ZeroPortfolioProvider
	activity *risk.DailyActivity
}

func (p guardPortfolio) DailyActivity(context.Context) (risk.DailyActivity, error) {
	return *p.activity, nil
}

func TestDailyGuard_LossLimit_StopsEntriesAtBoundary(t *testing.T) {
	f := newGuardFixture(t, map[string]string{dailyguardsettings.KeyLossLimitJPY: "50000"})

	f.wantPass(-49_999, 0)
	f.wantHalt(-50_000, 0, domain.DailyHaltReasonLossLimit)
	if len(f.notifier.halts) != 1 || f.notifier.halts[0].Reason != domain.DailyHaltReasonLossLimit {
		t.Fatalf("notifications = %+v, want one loss-limit halt", f.notifier.halts)
	}
}

func TestDailyGuard_Halt_IsLatchedAndNotifiedOnce(t *testing.T) {
	f := newGuardFixture(t, map[string]string{dailyguardsettings.KeyLossLimitJPY: "50000"})

	f.wantHalt(-60_000, 0, domain.DailyHaltReasonLossLimit)
	// Unrealized loss recovers: the day stays closed.
	f.wantHalt(-1_000, 0, domain.DailyHaltReasonLossLimit)
	f.wantHalt(0, 0, domain.DailyHaltReasonLossLimit)
	if len(f.notifier.halts) != 1 {
		t.Fatalf("notifications = %d, want exactly 1 for one halt", len(f.notifier.halts))
	}
}

func TestDailyGuard_Halt_DoesNotRaiseKillSwitch(t *testing.T) {
	f := newGuardFixture(t, map[string]string{dailyguardsettings.KeyLossLimitJPY: "50000"})
	f.wantHalt(-60_000, 0, domain.DailyHaltReasonLossLimit)

	state, events, err := f.engine.State(context.Background())
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	if state != domain.SystemStateRunning || len(events) != 0 {
		t.Fatalf("State = %v with %d kill switch events, want running with none (the halt only stops entries; exits stay allowed)", state, len(events))
	}
}

func TestDailyGuard_ProfitLock_StopsEntriesAfterGivebackFromPeak(t *testing.T) {
	f := newGuardFixture(t, map[string]string{
		dailyguardsettings.KeyProfitLockTriggerJPY:  "10000",
		dailyguardsettings.KeyProfitLockGivebackPct: "30",
	})

	f.wantPass(12_000, 0) // peak 12,000: the lock is armed
	f.wantPass(8_401, 0)  // gave back 29.9%
	f.wantHalt(8_400, 0, domain.DailyHaltReasonProfitLock)
	f.wantHalt(20_000, 0, domain.DailyHaltReasonProfitLock) // latched even if profit recovers
	if len(f.notifier.halts) != 1 {
		t.Fatalf("notifications = %d, want 1", len(f.notifier.halts))
	}
}

func TestDailyGuard_ProfitLock_NotArmedBelowTrigger(t *testing.T) {
	f := newGuardFixture(t, map[string]string{
		dailyguardsettings.KeyProfitLockTriggerJPY:  "10000",
		dailyguardsettings.KeyProfitLockGivebackPct: "30",
	})

	f.wantPass(9_999, 0) // peak stays below the trigger
	f.wantPass(0, 0)     // a full give-back does not halt: the lock never armed
	f.wantPass(-20_000, 0)
}

func TestDailyGuard_ProfitLock_NeedsBothThresholds(t *testing.T) {
	f := newGuardFixture(t, map[string]string{dailyguardsettings.KeyProfitLockTriggerJPY: "10000"})

	f.wantPass(15_000, 0)
	f.wantPass(0, 0)
}

func TestDailyGuard_MaxTrades_StopsEntriesAtBoundary(t *testing.T) {
	f := newGuardFixture(t, map[string]string{dailyguardsettings.KeyMaxTrades: "5"})

	f.wantPass(0, 4)
	f.wantHalt(0, 5, domain.DailyHaltReasonMaxTrades)
}

func TestDailyGuard_ZeroOrUnsetDisablesEveryRule(t *testing.T) {
	f := newGuardFixture(t, map[string]string{
		dailyguardsettings.KeyLossLimitJPY:          "0",
		dailyguardsettings.KeyProfitLockTriggerJPY:  "0",
		dailyguardsettings.KeyProfitLockGivebackPct: "0",
		dailyguardsettings.KeyMaxTrades:             "0",
	})

	f.wantPass(-10_000_000, 1000)
	f.wantPass(10_000_000, 1000)
	if len(f.notifier.halts) != 0 {
		t.Fatalf("notifications = %d, want 0", len(f.notifier.halts))
	}
}

func TestDailyGuard_ResetsOnNextTradingDay(t *testing.T) {
	f := newGuardFixture(t, map[string]string{dailyguardsettings.KeyLossLimitJPY: "50000", dailyguardsettings.KeyMaxTrades: "5"})
	*f.now = jst(23, 59)
	f.wantHalt(-60_000, 5, domain.DailyHaltReasonLossLimit)
	*f.now = jst(23, 59).Add(59 * time.Second) // 23:59:59 JST: still the same trading day
	f.wantHalt(0, 0, domain.DailyHaltReasonLossLimit)

	*f.now = jst(23, 59).Add(time.Minute) // 00:00 JST next day
	f.wantPass(0, 0)
	// The new day starts from scratch: the same figures halt again, notified again.
	f.wantHalt(-50_000, 0, domain.DailyHaltReasonLossLimit)
	if len(f.notifier.halts) != 2 || f.notifier.halts[1].Day != "2026-10-15" {
		t.Fatalf("notifications = %+v, want a second halt for 2026-10-15", f.notifier.halts)
	}
}

func TestDailyGuard_PeakDoesNotCarryOverToNextDay(t *testing.T) {
	f := newGuardFixture(t, map[string]string{
		dailyguardsettings.KeyProfitLockTriggerJPY:  "10000",
		dailyguardsettings.KeyProfitLockGivebackPct: "30",
	})
	f.wantPass(50_000, 0) // a big peak on day 1

	*f.now = jst(10, 0).AddDate(0, 0, 1)
	f.wantPass(0, 0) // day 2 starts with peak 0: no lock
	f.wantPass(5_000, 0)
}

func TestDailyGuard_DisablingTheRuleLiftsItsHalt(t *testing.T) {
	f := newGuardFixture(t, map[string]string{dailyguardsettings.KeyLossLimitJPY: "50000"})
	f.wantHalt(-60_000, 0, domain.DailyHaltReasonLossLimit)

	f.set(dailyguardsettings.KeyLossLimitJPY, "0")
	f.wantPass(-60_000, 0)
}

func TestDailyGuard_FailsClosedWhenActivityUnreadable(t *testing.T) {
	db := newTestDB(t)
	snapshots := market.NewSnapshotRepository(db)
	seedSnapshot(t, db, snapshots, 2000, 5)
	settings := system.NewRuntimeSettingsRepository(db)
	e := risk.NewEngine(risk.Config{
		Limits: testLimits(), KillSwitch: system.NewKillSwitchRepository(db), Settings: settings, Snapshots: snapshots,
		Portfolio: erroring{failing: "DailyActivity"},
	})

	// Rule disabled: the unreadable activity is never consulted.
	if passed, reason := e.Check(context.Background(), 1, domain.JevDirectionLong); !passed {
		t.Fatalf("Check with all rules off rejected: %s", reason)
	}
	if err := settings.Set(context.Background(), dailyguardsettings.KeyMaxTrades, "5", time.Now()); err != nil {
		t.Fatalf("set: %v", err)
	}
	passed, reason := e.Check(context.Background(), 1, domain.JevDirectionLong)
	if passed || !strings.HasPrefix(reason, risk.ReasonRiskEngineError) {
		t.Fatalf("Check = (%v, %q), want fail closed with %s", passed, reason, risk.ReasonRiskEngineError)
	}
}

func TestDailyGuard_CheckDailyGuard_HaltsWithoutACandidate(t *testing.T) {
	f := newGuardFixture(t, map[string]string{dailyguardsettings.KeyMaxTrades: "3"})
	*f.activity = risk.DailyActivity{Trades: 3}

	if err := f.engine.CheckDailyGuard(context.Background()); err != nil {
		t.Fatalf("CheckDailyGuard: %v", err)
	}
	if len(f.notifier.halts) != 1 || f.notifier.halts[0].Reason != domain.DailyHaltReasonMaxTrades {
		t.Fatalf("notifications = %+v, want one max-trades halt", f.notifier.halts)
	}
	halt, err := f.engine.DailyHalt(context.Background())
	if err != nil || halt == nil || halt.Reason != domain.DailyHaltReasonMaxTrades || halt.Day != "2026-10-14" {
		t.Fatalf("DailyHalt = (%+v, %v), want today's max-trades halt", halt, err)
	}
	if !strings.Contains(halt.Message, "3回") {
		t.Errorf("halt.Message = %q, want it to state the figures", halt.Message)
	}

	// Next trading day: the screen shows nothing.
	*f.now = f.now.AddDate(0, 0, 1)
	if halt, err := f.engine.DailyHalt(context.Background()); err != nil || halt != nil {
		t.Fatalf("DailyHalt next day = (%+v, %v), want (nil, nil)", halt, err)
	}
}

func TestDailyGuard_NotificationFailureStillHalts(t *testing.T) {
	f := newGuardFixture(t, map[string]string{dailyguardsettings.KeyMaxTrades: "1"})
	f.notifier.err = errors.New("slack down")
	f.wantHalt(0, 1, domain.DailyHaltReasonMaxTrades)
}
