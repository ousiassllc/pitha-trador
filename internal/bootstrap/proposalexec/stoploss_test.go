package proposalexec_test

import (
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap/proposalexec"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/domain/orderproposal"
)

// A buy whose stop is at or above the price it is expected to fill at is
// rejected, not quietly given the application's stop (issue #857). The latest
// price of the fixture is 2500.
func TestOpenLong_MarketStopAtOrAboveLatestPriceIsRejected(t *testing.T) {
	for name, stop := range map[string]float64{"equal to the latest price": 2500, "above the latest price": 2600} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			id := f.propose(spec{stop: stop})
			t.Log("before:\n" + f.dump(`SELECT id, status, status_reason, trade_signal_id, effective_stop_loss_price FROM order_proposals`) +
				f.dump(`SELECT count(*) FROM trade_signals WHERE policy_version='mcp-proposal-v1'`))
			f.cycle()
			p := f.wantEnd(id, orderproposal.StatusRejected, proposalexec.ReasonStopLossNotBelowPrice)
			t.Log("after:\n" + f.dump(`SELECT id, status, status_reason, trade_signal_id, effective_stop_loss_price FROM order_proposals`) +
				f.dump(`SELECT count(*) FROM trade_signals WHERE policy_version='mcp-proposal-v1'`))
			if p.TradeSignalID != nil || p.PaperOrderID != nil || p.PositionID != nil {
				t.Fatalf("links = signal %v order %v position %v, want none", p.TradeSignalID, p.PaperOrderID, p.PositionID)
			}
			for _, table := range []string{"trade_signals", "paper_orders", "positions"} {
				if n := f.count(table, ""); n != 0 {
					t.Fatalf("%s has %d rows, want 0", table, n)
				}
			}
			if f.risk.checks != 0 || f.exec.enters != 0 {
				t.Fatalf("CheckThenEnter calls = %d, Enter calls = %d; want 0 and 0", f.risk.checks, f.exec.enters)
			}
			var detail string
			if err := f.db.QueryRow(`SELECT detail_json FROM order_proposal_events WHERE proposal_id = ? AND reason_code = ?`,
				id, proposalexec.ReasonStopLossNotBelowPrice).Scan(&detail); err != nil {
				t.Fatalf("no %s event: %v", proposalexec.ReasonStopLossNotBelowPrice, err)
			}
			want := map[float64]string{
				2500: `{"latest_price":2500,"expected_entry_price":2500,"stop_loss_price":2500}`,
				2600: `{"latest_price":2500,"expected_entry_price":2500,"stop_loss_price":2600}`,
			}[stop]
			if detail != want {
				t.Fatalf("detail_json = %s, want %s", detail, want)
			}
		})
	}
}

// Below the latest price the proposal goes on as before: the farther of its
// stop and the application's is what the maximum loss is judged against.
func TestOpenLong_MarketStopBelowLatestPriceIsProcessedAsBefore(t *testing.T) {
	f := newFixture(t)
	id := f.propose(spec{stop: 2499})
	f.cycle()
	p := f.wantEnd(id, orderproposal.StatusFilled, "")
	var stop float64
	if err := f.db.QueryRow(`SELECT effective_stop_loss_price FROM order_proposals WHERE id = ?`, id).Scan(&stop); err != nil {
		t.Fatal(err)
	}
	// The app's stop is 0.6% below 2500 = 2485: farther than 2499, so it is used.
	if stop != 2485 || p.TradeSignalID == nil {
		t.Fatalf("effective stop = %v, signal = %v; want 2485 and a signal", stop, p.TradeSignalID)
	}
	if f.risk.checks != 1 || f.exec.enters != 1 {
		t.Fatalf("CheckThenEnter calls = %d, Enter calls = %d; want 1 and 1", f.risk.checks, f.exec.enters)
	}
}

// A limit order's expected fill is the limit when that is below the latest
// price (the MCP already refuses a stop at or above such a limit, so the check
// never fires there), and the latest price when the limit is above it: the
// order fills at once, so the same check applies.
func TestOpenLong_LimitStopIsJudgedAgainstTheExpectedFill(t *testing.T) {
	cases := []struct {
		name         string
		limit, stop  float64
		wantStatus   string
		wantReason   string
		wantRiskRuns int
	}{
		{"limit above latest, stop above latest", 3000, 2600, orderproposal.StatusRejected, proposalexec.ReasonStopLossNotBelowPrice, 0},
		{"limit above latest, stop equal to latest", 3000, 2500, orderproposal.StatusRejected, proposalexec.ReasonStopLossNotBelowPrice, 0},
		{"limit above latest, stop below latest", 3000, 2400, orderproposal.StatusFilled, "", 1},
		{"limit below latest, stop below the limit", 2400, 2300, orderproposal.StatusSubmitted, "", 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			id := f.propose(spec{orderType: domain.OrderTypeLimit, limit: tc.limit, stop: tc.stop})
			f.cycle()
			f.wantEnd(id, tc.wantStatus, tc.wantReason)
			if f.risk.checks != tc.wantRiskRuns {
				t.Fatalf("CheckThenEnter calls = %d, want %d", f.risk.checks, tc.wantRiskRuns)
			}
		})
	}
}

// The check runs after the stale-quote one: a stale quote is still reported as
// such, whatever the stop.
func TestOpenLong_StaleQuoteIsReportedBeforeTheStopLossCheck(t *testing.T) {
	f := newFixture(t)
	*f.clock = at(10, 30, 0)
	id := f.propose(spec{stop: 2600, expires: at(11, 30, 0)})
	f.cycle()
	f.wantEnd(id, orderproposal.StatusRejected, proposalexec.ReasonStaleQuote)
	if f.risk.checks != 0 || f.exec.enters != 0 {
		t.Fatalf("CheckThenEnter calls = %d, Enter calls = %d; want 0 and 0", f.risk.checks, f.exec.enters)
	}
}
