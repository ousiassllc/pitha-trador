package proposaltests

import (
	"net/http"
	"slices"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/proposal"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

// outcomesServer is a server over the database of testkit.SeedProposalOutcomes:
// an expired, a cancelled, a rejected, a filled-and-won, a filled-and-lost and
// a close proposal. The intake switch stays off: the read tools do not need it.
func outcomesServer(t *testing.T) *testServer {
	t.Helper()
	srv, w := proposalServer(t)
	testkit.SeedProposalOutcomes(t, w)
	return srv
}

type listed struct {
	Proposals  []proposal.Item `json:"proposals"`
	NextCursor *string         `json:"next_cursor"`
}

func listIDs(t *testing.T, srv *testServer, args map[string]any) (ids []int64, next *string) {
	t.Helper()
	var out listed
	srv.call(t, "order_proposals_list", args).data(t, "order_proposals_list", &out)
	for _, p := range out.Proposals {
		ids = append(ids, p.ProposalID)
	}
	return ids, out.NextCursor
}

func near(t *testing.T, name string, got *float64, want float64) {
	t.Helper()
	if got == nil || *got != want {
		t.Errorf("%s = %v, want %v", name, got, want)
	}
}

// order_proposals_list: newest first, the brief row of each way a proposal can
// end, and the realized pnl only once the linked position has closed.
func TestOrderProposalsListShowsEachOutcome(t *testing.T) {
	srv := outcomesServer(t)
	var out listed
	srv.call(t, "order_proposals_list", map[string]any{}).data(t, "order_proposals_list", &out)
	var ids []int64
	byID := map[int64]proposal.Item{}
	for _, p := range out.Proposals {
		ids = append(ids, p.ProposalID)
		byID[p.ProposalID] = p
	}
	if !slices.Equal(ids, []int64{6, 5, 4, 3, 2, 1}) || out.NextCursor != nil {
		t.Fatalf("ids = %v next = %v", ids, out.NextCursor)
	}

	win := byID[testkit.ProposalWin]
	if win.Status != "filled" || win.Symbol != "7203" || win.Bucket != "stock" || win.Action != "open_long" ||
		win.ReceivedAt != "2026-10-17T00:00:00Z" || win.PositionID == nil || *win.PositionID != 1 ||
		win.AppliedQuantity == nil || *win.AppliedQuantity != 100 || win.Quantity == nil || *win.Quantity != 200 {
		t.Errorf("won proposal = %+v", win)
	}
	near(t, "won proposal pnl", win.RealizedPnlJPY, 20970)
	near(t, "won proposal estimate", win.ComputedMaxLossJPY, 15000)
	near(t, "lost proposal pnl", byID[testkit.ProposalLoss].RealizedPnlJPY, -12030)

	rejected := byID[testkit.ProposalRejected]
	if rejected.Status != "rejected" || rejected.StatusReason != "bucket_budget_exceeded" || rejected.PositionID != nil || rejected.RealizedPnlJPY != nil {
		t.Errorf("rejected proposal = %+v", rejected)
	}
	if got := byID[testkit.ProposalCancelled]; got.Status != "cancelled" || got.Bucket != "bond_etf" || got.StatusReason != "cancel_requested" {
		t.Errorf("cancelled proposal = %+v", got)
	}
	if got := byID[testkit.ProposalExpired]; got.Status != "expired" || got.StatusReason != "expired" {
		t.Errorf("expired proposal = %+v", got)
	}
	if got := byID[testkit.ProposalClose]; got.Action != "close" || got.Status != "closed" {
		t.Errorf("close proposal = %+v", got)
	}
}

// from/to are on the day the proposal was received (JST), and status, bucket
// and paging narrow the list.
func TestOrderProposalsListFiltersAndPages(t *testing.T) {
	srv := outcomesServer(t)
	cases := []struct {
		name string
		args map[string]any
		want []int64
	}{
		{"period", map[string]any{"from": "2026-10-17", "to": "2026-10-18"}, []int64{6, 5, 4}},
		{"empty period", map[string]any{"from": "2026-09-20", "to": "2026-09-21"}, nil},
		{"status", map[string]any{"status": "rejected"}, []int64{3}},
		{"bucket", map[string]any{"bucket": "bond_etf"}, []int64{2}},
		{"status and bucket", map[string]any{"status": "filled", "bucket": "bond_etf"}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got, next := listIDs(t, srv, c.args); !slices.Equal(got, c.want) || next != nil {
				t.Errorf("ids = %v next = %v, want %v", got, next, c.want)
			}
		})
	}

	var got []int64
	args := map[string]any{"limit": 2}
	for range 5 {
		ids, next := listIDs(t, srv, args)
		got = append(got, ids...)
		if next == nil {
			break
		}
		args = map[string]any{"limit": 2, "cursor": *next}
	}
	if !slices.Equal(got, []int64{6, 5, 4, 3, 2, 1}) {
		t.Errorf("paged ids = %v", got)
	}
}

func TestOrderProposalsListRejectsBadArguments(t *testing.T) {
	srv := outcomesServer(t)
	for name, args := range map[string]map[string]any{
		"unknown status": {"status": "approved"},
		"unknown bucket": {"bucket": "fx"},
		"bad cursor":     {"cursor": "not-a-cursor"},
		"limit zero":     {"limit": 0},
		"unknown member": {"symbol": "7203"},
	} {
		t.Run(name, func(t *testing.T) {
			srv.call(t, "order_proposals_list", args).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidArgument)
		})
	}
	srv.call(t, "order_proposals_list", map[string]any{"limit": 201}).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeLimitExceeded)
}

func getProposal(t *testing.T, srv *testServer, id int64) proposal.State {
	t.Helper()
	var p proposal.State
	srv.call(t, "order_proposal_get", map[string]any{"proposal_id": id}).data(t, "order_proposal_get", &p)
	return p
}

// order_proposal_get follows a proposal to its trade signal, paper order and
// position (with the realized pnl as stored) and sets the loss the proposer
// expected next to the application's estimate and the actual one.
func TestOrderProposalGetTracesProposalToRealizedPnl(t *testing.T) {
	srv := outcomesServer(t)
	p := getProposal(t, srv, testkit.ProposalWin)
	if p.Status != "filled" || p.Rationale != "決算が好調で需給も良い" || p.ExitPlan == nil || *p.ExitPlan != "引けで手じまう" ||
		len(p.Sources) != 1 || p.Sources[0].URL != "https://example.com/ir" {
		t.Fatalf("proposal = %+v", p)
	}
	r := p.Result
	if r.Signal == nil || r.Signal.ID != 1 || r.Signal.PolicyVersion != "mcp-proposal-v1" || !r.Signal.RiskPassed || r.Signal.RejectCode != nil {
		t.Errorf("signal = %+v", r.Signal)
	}
	if r.Order == nil || r.Order.ID != 1 || r.Order.Side != "BUY" || r.Order.Status != "FILLED" || r.Order.Quantity != 100 ||
		r.Order.FeesJPY != 10 || r.Order.FilledPrice == nil || *r.Order.FilledPrice != 2500 {
		t.Errorf("order = %+v", r.Order)
	}
	pos := r.Position
	if pos == nil || pos.ID != 1 || pos.Status != "closed" || pos.Quantity != 100 || pos.EntryPrice != 2500 ||
		pos.ExitReason == nil || *pos.ExitReason != "take_profit" || pos.HoldingMinutes == nil || *pos.HoldingMinutes != 300 ||
		pos.ClosedAt == nil || *pos.ClosedAt != "2026-10-17T05:00:00Z" {
		t.Fatalf("position = %+v", pos)
	}
	near(t, "exit price", pos.ExitPrice, 2710)
	near(t, "realized pnl", pos.RealizedPnl, 20970)
	near(t, "fees", pos.FeesJPY, 30)

	near(t, "max loss proposed", r.MaxLoss.ProposedJPY, 20000)
	near(t, "max loss estimated", r.MaxLoss.EstimatedJPY, 15000)
	near(t, "actual loss of a gain", r.MaxLoss.ActualLossJPY, 0)
	near(t, "effective stop", r.MaxLoss.EffectiveStop, 2400)
	if r.StopLossReached == nil || *r.StopLossReached || r.TakeProfitReached == nil || !*r.TakeProfitReached {
		t.Errorf("stop reached = %v, take reached = %v; want false, true", r.StopLossReached, r.TakeProfitReached)
	}

	if len(p.Events) != 3 || p.Events[2].ToStatus != "filled" || p.Events[2].ReasonCode != "quantity_reduced" ||
		string(p.Events[2].Detail) != `{"requested_quantity":200,"applied_quantity":100}` || string(p.Events[0].Detail) != "null" {
		t.Errorf("events = %+v", p.Events)
	}
}

func TestOrderProposalGetLossReachedTheStop(t *testing.T) {
	srv := outcomesServer(t)
	r := getProposal(t, srv, testkit.ProposalLoss).Result
	near(t, "actual loss", r.MaxLoss.ActualLossJPY, 12030)
	near(t, "max loss estimated", r.MaxLoss.EstimatedJPY, 15000)
	if r.StopLossReached == nil || !*r.StopLossReached || r.TakeProfitReached == nil || *r.TakeProfitReached {
		t.Errorf("stop reached = %v, take reached = %v; want true, false", r.StopLossReached, r.TakeProfitReached)
	}
	if r.Position == nil || r.Position.ExitReason == nil || *r.Position.ExitReason != "proposal_close" {
		t.Errorf("position = %+v", r.Position)
	}
	// The close proposal points at the same position and the order that closed it.
	c := getProposal(t, srv, testkit.ProposalClose)
	if c.PositionID == nil || *c.PositionID != 2 || c.Result.Order == nil || c.Result.Order.Side != "SELL" ||
		c.Result.Position == nil || c.Result.Position.RealizedPnl == nil || *c.Result.Position.RealizedPnl != -12030 {
		t.Errorf("close proposal = %+v", c)
	}
	if c.Result.StopLossReached != nil || c.Result.TakeProfitReached != nil {
		t.Errorf("a close proposal has no stop or take-profit to reach: %+v", c.Result)
	}
}

// A proposal that went nowhere has what the application recorded and nothing
// else: a rejection keeps its signal's Risk code (not the reason text).
func TestOrderProposalGetOfRejectedExpiredAndCancelled(t *testing.T) {
	srv := outcomesServer(t)
	rej := getProposal(t, srv, testkit.ProposalRejected)
	if rej.Status != "rejected" || rej.StatusReason != "bucket_budget_exceeded" || rej.Result.Order != nil || rej.Result.Position != nil {
		t.Fatalf("rejected = %+v", rej)
	}
	if s := rej.Result.Signal; s == nil || s.RiskPassed || s.RejectCode == nil || *s.RejectCode != "bucket_budget_exceeded" {
		t.Errorf("rejected signal = %+v", s)
	}
	if rej.Result.MaxLoss.ActualLossJPY != nil || rej.Result.StopLossReached != nil || len(rej.Events) != 3 {
		t.Errorf("rejected result = %+v events %d", rej.Result, len(rej.Events))
	}

	exp := getProposal(t, srv, testkit.ProposalExpired)
	if exp.Status != "expired" || exp.Result.Signal != nil || exp.Result.Order != nil || exp.Result.Position != nil || len(exp.Events) != 2 {
		t.Errorf("expired = %+v", exp)
	}
	can := getProposal(t, srv, testkit.ProposalCancelled)
	if can.Status != "cancelled" || can.CancelRequestedAt == nil || *can.CancelRequestedAt != "2026-10-16T00:30:00Z" || len(can.Events) != 3 {
		t.Errorf("cancelled = %+v", can)
	}
}

// order_proposal_summary counts by status, rejection reason and bucket, and
// totals only the open_long proposals that filled (the close proposal's pnl is
// its entry proposal's already).
func TestOrderProposalSummaryOfAllOutcomes(t *testing.T) {
	srv := outcomesServer(t)
	var s proposal.Summary
	srv.call(t, "order_proposal_summary", map[string]any{}).data(t, "order_proposal_summary", &s)
	if s.TotalCount != 6 {
		t.Errorf("total = %d", s.TotalCount)
	}
	wantStatus := map[string]int{"expired": 1, "cancelled": 1, "rejected": 1, "filled": 2, "closed": 1}
	if len(s.ByStatus) != 10 {
		t.Errorf("by_status lists %d statuses, want all 10: %+v", len(s.ByStatus), s.ByStatus)
	}
	for _, c := range s.ByStatus {
		if c.Count != wantStatus[c.Code] {
			t.Errorf("status %s = %d, want %d", c.Code, c.Count, wantStatus[c.Code])
		}
	}
	if len(s.ByRejectReason) != 1 || s.ByRejectReason[0] != (proposal.Count{Code: "bucket_budget_exceeded", Count: 1}) {
		t.Errorf("by_reject_reason = %+v", s.ByRejectReason)
	}
	if len(s.ByBucket) != 2 || s.ByBucket[0] != (proposal.Count{Code: "stock", Count: 5}) || s.ByBucket[1] != (proposal.Count{Code: "bond_etf", Count: 1}) {
		t.Errorf("by_bucket = %+v", s.ByBucket)
	}
	f := s.Filled
	if f.ProposalCount != 2 || f.ClosedCount != 2 || f.OpenCount != 0 || f.WinCount != 1 || f.LossCount != 1 || f.TotalPnlJPY != 8940 {
		t.Errorf("filled = %+v", f)
	}
	near(t, "win rate", f.WinRate, 0.5)
	near(t, "expectancy", f.ExpectancyJPY, 4470)

	// A period with no proposals still has the whole shape, with nulls.
	var empty proposal.Summary
	srv.call(t, "order_proposal_summary", map[string]any{"from": "2026-09-20", "to": "2026-09-21"}).data(t, "order_proposal_summary", &empty)
	if empty.TotalCount != 0 || len(empty.ByStatus) != 10 || empty.Filled.WinRate != nil || empty.Filled.ExpectancyJPY != nil || empty.ByRejectReason == nil {
		t.Errorf("empty summary = %+v", empty)
	}
}

// A filled proposal whose position is still open is counted but has no win rate
// yet.
func TestOrderProposalSummaryOpenPositionHasNoResultYet(t *testing.T) {
	srv, w := proposalServer(t)
	testkit.TradeSignal(t, w, 1, "2026-10-17T00:00:04Z", "mcp-proposal-v1", true, "")
	testkit.PaperOrder(t, w, 1, "BUY", 1, 100, 2500, 10, "2026-10-17T00:00:06Z")
	testkit.ClosedPosition(t, w, 1, 1, nil, 100, 2500, "2026-10-17T00:00:06Z", "", nil, "")
	testkit.Proposal(t, w, testkit.ProposalRow{ID: 1, ReceivedAt: "2026-10-17T00:00:00Z", Status: "filled", Quantity: 100, Stop: 2400.0,
		SignalID: 1, OrderID: 1, PositionID: 1})
	var s proposal.Summary
	srv.call(t, "order_proposal_summary", map[string]any{}).data(t, "order_proposal_summary", &s)
	if s.Filled.ProposalCount != 1 || s.Filled.OpenCount != 1 || s.Filled.ClosedCount != 0 || s.Filled.WinRate != nil || s.Filled.TotalPnlJPY != 0 {
		t.Errorf("filled = %+v", s.Filled)
	}
	p := getProposal(t, srv, 1)
	if p.Result.Position == nil || p.Result.Position.Status != "open" || p.Result.Position.RealizedPnl != nil || p.Result.MaxLoss.ActualLossJPY != nil {
		t.Errorf("open position result = %+v", p.Result)
	}
}
