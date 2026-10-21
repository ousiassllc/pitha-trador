package proposalexec_test

import (
	"context"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap/proposalexec"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/domain/orderproposal"
)

func TestOpenLong_FilledProposalLinksSignalOrderAndPosition(t *testing.T) {
	f := newFixture(t)
	id := f.propose(spec{quantity: 100, stop: 2450})
	f.cycle()
	p := f.wantEnd(id, orderproposal.StatusFilled, "")
	if p.TradeSignalID == nil || p.PaperOrderID == nil || p.PositionID == nil {
		t.Fatalf("links = signal %v order %v position %v, want all set", p.TradeSignalID, p.PaperOrderID, p.PositionID)
	}
	sig, _ := f.svc.Signals.Get(context.Background(), *p.TradeSignalID)
	if sig.PolicyVersion != "mcp-proposal-v1" || !sig.RiskPassed || sig.JevDecisionID != nil || sig.RejectReason != nil {
		t.Fatalf("signal = %+v, want a passed mcp-proposal-v1 LONG row with no Jev decision", sig)
	}
	pos, err := f.svc.Positions.GetOpenByInstrument(context.Background(), f.inst.ID)
	if err != nil || pos.Quantity != 100 || pos.ID != *p.PositionID {
		t.Fatalf("position = (%+v, %v), want 100 shares, id %d", pos, err, *p.PositionID)
	}
	want := []string{">received", "received>processing", "processing>filled"}
	if got := f.events(id); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("events = %v, want %v", got, want)
	}
}

// The proposal asks for more than the Risk sizing allows (200 shares at 2,500
// yen under the 2% per-symbol limit): the order is the smaller number and the
// reduction is an event of its own.
func TestOpenLong_QuantityAboveSizingIsReducedAndTheReductionIsRecorded(t *testing.T) {
	f := newFixture(t)
	id := f.propose(spec{quantity: 1000})
	f.cycle()
	p := f.wantEnd(id, orderproposal.StatusFilled, "")
	pos, _ := f.svc.Positions.Get(context.Background(), *p.PositionID)
	if pos.Quantity != 200 {
		t.Fatalf("ordered %d shares, want 200 (PositionSize), not the requested 1000", pos.Quantity)
	}
	var detail string
	if err := f.db.QueryRow(`SELECT detail_json FROM order_proposal_events WHERE proposal_id = ? AND reason_code = ?`,
		id, proposalexec.ReasonQuantityReduced).Scan(&detail); err != nil {
		t.Fatalf("no %s event: %v", proposalexec.ReasonQuantityReduced, err)
	}
	if want := `{"requested_quantity":1000,"applied_quantity":200,"limited_by":"position_size"}`; detail != want {
		t.Fatalf("detail_json = %s, want %s", detail, want)
	}
	var applied int64
	var loss, stop float64
	if err := f.db.QueryRow(`SELECT applied_quantity, computed_max_loss_jpy, effective_stop_loss_price FROM order_proposals WHERE id = ?`, id).Scan(&applied, &loss, &stop); err != nil {
		t.Fatal(err)
	}
	// The proposal's stop (2400) is nearer than the app's (0.6% below 2500 = 2485); the farther one, 2400, is used.
	if applied != 200 || stop != 2400 || loss != 20000 {
		t.Fatalf("applied=%d stop=%v max_loss=%v, want 200 / 2400 / 20000", applied, stop, loss)
	}
}

func TestOpenLong_BudgetBecomesWholeTradingUnitsAtTheLatestPrice(t *testing.T) {
	f := newFixture(t)
	id := f.propose(spec{budget: 400_000}) // 160 shares -> 100
	f.cycle()
	p := f.wantEnd(id, orderproposal.StatusFilled, "")
	if pos, _ := f.svc.Positions.Get(context.Background(), *p.PositionID); pos.Quantity != 100 {
		t.Fatalf("ordered %d shares, want 100 (400,000 yen / 2,500 rounded down to a trading unit)", pos.Quantity)
	}
}

func TestOpenLong_BudgetBelowOneTradingUnitIsRejectedSizeZero(t *testing.T) {
	f := newFixture(t)
	id := f.propose(spec{budget: 100_000})
	f.cycle()
	f.wantEnd(id, orderproposal.StatusRejected, proposalexec.ReasonSizeZero)
	if f.count("paper_orders", "") != 0 || f.exec.enters != 0 {
		t.Fatal("an order went out for a zero-size proposal")
	}
}

// The application's stop is 0.6% below the entry; a proposal stop farther away
// than the max_trade_loss_pct allows (1% of 30M = 300,000 yen) is rejected.
func TestOpenLong_MaxLossAboveMaxTradeLossPctIsRejected(t *testing.T) {
	f := newFixture(t)
	id := f.propose(spec{stop: 500}) // 200 shares x (2,500 - 500) = 400,000 yen
	f.cycle()
	f.wantEnd(id, orderproposal.StatusRejected, proposalexec.ReasonMaxTradeLoss)
	var loss float64
	if err := f.db.QueryRow(`SELECT computed_max_loss_jpy FROM order_proposals WHERE id = ?`, id).Scan(&loss); err != nil || loss != 400000 {
		t.Fatalf("computed_max_loss_jpy = %v (%v), want 400000 recorded next to the proposal's own max_loss_jpy", loss, err)
	}
	if f.count("paper_orders", "") != 0 || f.exec.enters != 0 {
		t.Fatal("an order went out for a proposal over the max trade loss")
	}
}

func TestOpenLong_SymbolRules(t *testing.T) {
	for name, tc := range map[string]struct {
		prepare func(f *fixture)
		spec    spec
		reason  string
	}{
		"no such symbol":     {spec: spec{symbol: "9999"}, reason: proposalexec.ReasonUnknownSymbol},
		"bucket mismatch":    {spec: spec{bucket: domain.BucketBondETF}, reason: proposalexec.ReasonBucketMismatch},
		"inactive symbol":    {prepare: func(f *fixture) { f.setActive(false) }, reason: proposalexec.ReasonInactiveSymbol},
		"an index is no buy": {prepare: func(f *fixture) { f.setKind(domain.InstrumentKindMarketIndex) }, reason: proposalexec.ReasonUnknownSymbol},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			if tc.prepare != nil {
				tc.prepare(f)
			}
			id := f.propose(tc.spec)
			f.cycle()
			f.wantEnd(id, orderproposal.StatusRejected, tc.reason)
			if f.count("trade_signals", "") != 0 || f.count("paper_orders", "") != 0 {
				t.Fatal("a signal or an order was written for a symbol the application refuses")
			}
		})
	}
}

func TestOpenLong_StaleQuoteIsRejected(t *testing.T) {
	f := newFixture(t)
	*f.clock = at(10, 30, 0) // the 10:00:00 bar is 30 minutes old; the limit is 5
	id := f.propose(spec{expires: at(11, 30, 0)})
	f.cycle()
	f.wantEnd(id, orderproposal.StatusRejected, proposalexec.ReasonStaleQuote)
	if f.count("paper_orders", "") != 0 {
		t.Fatal("an order went out on a stale quote")
	}
}

// What Execution refuses per symbol is recorded, not just logged.
func TestOpenLong_ExecutionRefusalsAreRecorded(t *testing.T) {
	t.Run("position already open", func(t *testing.T) {
		f := newFixture(t)
		f.openPosition(100, 2500)
		id := f.propose(spec{})
		f.cycle()
		f.wantEnd(id, orderproposal.StatusRejected, proposalexec.ReasonPositionOpen)
	})
	t.Run("pending order exists", func(t *testing.T) {
		f := newFixture(t)
		first := f.propose(spec{orderType: domain.OrderTypeLimit, limit: 2000})
		f.cycle()
		f.wantEnd(first, orderproposal.StatusSubmitted, "")
		second := f.propose(spec{})
		f.cycle()
		f.wantEnd(second, orderproposal.StatusRejected, proposalexec.ReasonPendingOrder)
	})
	t.Run("symbol in cooldown", func(t *testing.T) {
		f := newFixture(t)
		pos := f.openPosition(100, 2500)
		// A loss on this symbol starts Execution's per-symbol cooldown.
		if _, err := f.svc.Execution.Close(context.Background(), pos.ID, domain.ExitReasonStopLoss, 2000, fillBook(), *f.clock); err != nil {
			t.Fatal(err)
		}
		id := f.propose(spec{})
		f.cycle()
		f.wantEnd(id, orderproposal.StatusRejected, proposalexec.ReasonSymbolInCooldown)
	})
}

// One rejected, one filled and one expired proposal, then the five tables: the
// evidence of #828.
func TestProposalRecords_RejectedFilledAndExpiredLeaveTheirRows(t *testing.T) {
	f := newFixture(t)
	rejected := f.propose(spec{symbol: "9999"})
	filled := f.propose(spec{quantity: 100})
	expired := f.propose(spec{expires: at(10, 0, 10)}) // already past at 10:00:30
	f.cycle()
	f.wantEnd(rejected, orderproposal.StatusRejected, proposalexec.ReasonUnknownSymbol)
	f.wantEnd(filled, orderproposal.StatusFilled, "")
	f.wantEnd(expired, orderproposal.StatusExpired, proposalexec.ReasonExpired)
	for _, q := range []string{
		`SELECT id, action, symbol, status, status_reason, instrument_id, trade_signal_id, paper_order_id, position_id, applied_quantity FROM order_proposals ORDER BY id`,
		`SELECT id, proposal_id, from_status, to_status, reason_code, actor FROM order_proposal_events ORDER BY id`,
		`SELECT id, symbol, direction, policy_version, risk_passed, reject_reason, jev_decision_id FROM trade_signals ORDER BY id`,
		`SELECT id, symbol, side, order_type, quantity, status, trade_signal_id FROM paper_orders ORDER BY id`,
		`SELECT id, symbol, side, quantity, entry_order_id, entry_price FROM positions ORDER BY id`,
	} {
		t.Logf("%s\n%s", q, f.dump(q))
	}
}
