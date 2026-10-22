package proposalexectests

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/bucketsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/dailyguardsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/domain/orderproposal"
	"github.com/ousiassllc/pitha-trador/internal/service/risk"
)

// wantRiskRecord fails unless the rejected proposal left a trade_signals row
// of its own policy_version with risk_passed = 0 and the Risk reason as
// Policy records it, and no order went out.
func wantRiskRecord(t *testing.T, f *fixture, id int64, reason string) {
	t.Helper()
	p := f.wantEnd(id, orderproposal.StatusRejected, reason)
	if p.TradeSignalID == nil {
		t.Fatalf("rejected proposal has no trade_signal_id")
	}
	sig, err := f.svc.Signals.Get(context.Background(), *p.TradeSignalID)
	if err != nil {
		t.Fatal(err)
	}
	if sig.PolicyVersion != orderproposal.PolicyVersion || sig.RiskPassed || sig.JevDecisionID != nil || sig.Direction != domain.JevDirectionLong {
		t.Fatalf("signal = %+v, want a LONG risk_passed=0 row of %s without a Jev decision", sig, orderproposal.PolicyVersion)
	}
	if sig.RejectReason == nil || !strings.HasPrefix(*sig.RejectReason, "risk_engine_rejected: "+reason) {
		t.Fatalf("reject_reason = %v, want it to start with risk_engine_rejected: %s", sig.RejectReason, reason)
	}
	if n := f.count("paper_orders", ""); n != 0 {
		t.Fatalf("paper_orders = %d, want none", n)
	}
	if f.exec.enters != 0 {
		t.Fatalf("Enter was called %d times for a proposal Risk rejected, want 0", f.exec.enters)
	}
}

func TestKillSwitchActive_OpenLongIsRejectedWithRiskReasonAndNoOrder(t *testing.T) {
	f := newFixture(t)
	if err := f.svc.Risk.Kill(context.Background()); err != nil {
		t.Fatal(err)
	}
	id := f.propose(spec{})
	f.cycle()
	wantRiskRecord(t, f, id, risk.ReasonKillSwitchActive)
	t.Logf("kill switch: events=%v\n%s\n%s", f.events(id),
		f.dump(`SELECT id, status, status_reason, trade_signal_id, paper_order_id FROM order_proposals`),
		f.dump(`SELECT id, symbol, direction, policy_version, risk_passed, reject_reason FROM trade_signals`))
}

func TestSystemPaused_OpenLongIsRejectedWithRiskReasonAndNoOrder(t *testing.T) {
	f := newFixture(t)
	if err := f.svc.Risk.Pause(context.Background()); err != nil {
		t.Fatal(err)
	}
	id := f.propose(spec{})
	f.cycle()
	wantRiskRecord(t, f, id, risk.ReasonSystemPaused)
}

// A daily guard that already halted the day rejects with its own reason, the
// one Policy's signals show.
func TestDailyGuard_OpenLongIsRejectedWithTheHaltReason(t *testing.T) {
	for name, tc := range map[string]struct{ key, value, reason string }{
		"loss limit": {dailyguardsettings.KeyLossLimitJPY, "50000", domain.DailyHaltReasonLossLimit},
		"max trades": {dailyguardsettings.KeyMaxTrades, "3", domain.DailyHaltReasonMaxTrades},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.haltDailyGuard(tc.key, tc.value, tc.reason)
			id := f.propose(spec{})
			f.cycle()
			wantRiskRecord(t, f, id, tc.reason)
		})
	}
}

// haltDailyGuard enables the rule behind key and latches today's (JST) halt
// for reason, the way Check leaves it once the rule is reached.
func (f *fixture) haltDailyGuard(key, value, reason string) {
	f.t.Helper()
	ctx := context.Background()
	if err := f.svc.Settings.Set(ctx, key, value, time.Now()); err != nil {
		f.t.Fatal(err)
	}
	day := time.Now().In(time.FixedZone("JST", 9*60*60)).Format("2006-01-02")
	state := `{"day":"` + day + `","halt_reason":"` + reason + `","halt_message":"test","halted_at":"` + time.Now().UTC().Format(time.RFC3339Nano) + `"}`
	if err := f.svc.Settings.Set(ctx, "system.daily_guard", state, time.Now()); err != nil {
		f.t.Fatal(err)
	}
}

func TestStockBucketZero_OpenLongIsRejectedWithBucketDisabled(t *testing.T) {
	f := newFixture(t)
	if err := f.svc.Settings.Set(context.Background(), bucketsettings.KeyStockBudgetJPY, "0", time.Now()); err != nil {
		t.Fatal(err)
	}
	id := f.propose(spec{})
	f.cycle()
	wantRiskRecord(t, f, id, risk.ReasonBucketDisabled)
}

func TestStockBucketOverBudget_OpenLongIsRejectedWithBucketBudgetExceeded(t *testing.T) {
	f := newFixture(t)
	// 200 shares at 2,500 yen is 500,000 yen: more than the 100,000-yen budget.
	if err := f.svc.Settings.Set(context.Background(), bucketsettings.KeyStockBudgetJPY, "100000", time.Now()); err != nil {
		t.Fatal(err)
	}
	id := f.propose(spec{})
	f.cycle()
	wantRiskRecord(t, f, id, risk.ReasonBucketBudgetExceeded)
}

// Only a proposal Risk passed reaches Enter: three rejected for Risk reasons
// never call it, the one that passes calls it exactly once.
func TestEnterIsCalledOnlyForProposalsRiskPassed(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if err := f.svc.Settings.Set(ctx, bucketsettings.KeyStockBudgetJPY, "0", time.Now()); err != nil {
		t.Fatal(err)
	}
	f.propose(spec{})
	f.cycle()
	if f.exec.enters != 0 {
		t.Fatalf("Enter calls = %d after a Risk rejection, want 0", f.exec.enters)
	}
	if err := f.svc.Settings.Set(ctx, bucketsettings.KeyStockBudgetJPY, "10000000", time.Now()); err != nil {
		t.Fatal(err)
	}
	id := f.propose(spec{})
	f.cycle()
	f.wantEnd(id, orderproposal.StatusFilled, "")
	if f.exec.enters != 1 {
		t.Fatalf("Enter calls = %d, want 1 (only the proposal Risk passed)", f.exec.enters)
	}
}
