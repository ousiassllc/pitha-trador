package proposaltests

import (
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/origin"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// paper_trades says how each trade was entered, from the policy_version of the
// signal behind its entry order: the meta-labeling rule (with or without an
// applied proposal's suffix), the bootstrap rule, an order proposal, or nothing
// (no signal).
func TestPaperTradesOriginTellsTheThreeEntriesApart(t *testing.T) {
	srv := outcomesServer(t)
	var out struct {
		Trades []query.Trade `json:"trades"`
	}
	srv.call(t, "paper_trades", map[string]any{}).data(t, "paper_trades", &out)
	got := map[int64]string{}
	for _, tr := range out.Trades {
		got[tr.ID] = tr.Origin
	}
	want := map[int64]string{
		1: origin.OrderProposal, // entered by proposal 4 (policy mcp-proposal-v1)
		2: origin.OrderProposal, // entered by proposal 5, closed by a close proposal: still its entry's origin
		3: origin.MetaModel,     // policy-v2+sol-3
		4: origin.Bootstrap,     // policy-vb+sol-1
		5: origin.Unknown,       // entry order without a signal
	}
	if len(got) != len(want) {
		t.Fatalf("origins = %v, want %v", got, want)
	}
	for id, o := range want {
		if got[id] != o {
			t.Errorf("trade %d origin = %q, want %q", id, got[id], o)
		}
	}
}

// paper_performance_summary splits the closed trades by origin without
// changing its totals; every origin is listed even with no trade.
func TestPerformanceSummaryByOrigin(t *testing.T) {
	srv := outcomesServer(t)
	var p query.Performance
	srv.call(t, "paper_performance_summary", map[string]any{}).data(t, "paper_performance_summary", &p)
	if p.ClosedTradeCount != 5 || len(p.ByOrigin) != 4 {
		t.Fatalf("performance = %+v", p)
	}
	want := []origin.Stat{
		{Origin: origin.MetaModel, ClosedTradeCount: 1, WinCount: 1, TotalPnlJPY: 500},
		{Origin: origin.Bootstrap, ClosedTradeCount: 1, LossCount: 1, TotalPnlJPY: -300},
		{Origin: origin.OrderProposal, ClosedTradeCount: 2, WinCount: 1, LossCount: 1, TotalPnlJPY: 8940},
		{Origin: origin.Unknown, ClosedTradeCount: 1, WinCount: 1, TotalPnlJPY: 100},
	}
	sum := 0.0
	for i, w := range want {
		g := p.ByOrigin[i]
		if g.Origin != w.Origin || g.ClosedTradeCount != w.ClosedTradeCount || g.WinCount != w.WinCount || g.LossCount != w.LossCount || g.TotalPnlJPY != w.TotalPnlJPY {
			t.Errorf("by_origin[%d] = %+v, want %+v", i, g, w)
		}
		if g.WinRate == nil || g.ExpectancyJPY == nil {
			t.Errorf("by_origin[%d] has trades but no win_rate / expectancy: %+v", i, g)
		}
		sum += g.TotalPnlJPY
	}
	if sum != p.TotalPnlJPY {
		t.Errorf("by_origin totals %v != total_pnl_jpy %v", sum, p.TotalPnlJPY)
	}
	near(t, "order_proposal win rate", p.ByOrigin[2].WinRate, 0.5)
	near(t, "order_proposal expectancy", p.ByOrigin[2].ExpectancyJPY, 4470)

	var empty query.Performance
	srv.call(t, "paper_performance_summary", map[string]any{"from": "2026-09-20", "to": "2026-09-21"}).data(t, "paper_performance_summary", &empty)
	if len(empty.ByOrigin) != 4 || empty.ByOrigin[2].WinRate != nil || empty.ByOrigin[2].ExpectancyJPY != nil || empty.ByOrigin[2].ClosedTradeCount != 0 {
		t.Errorf("empty by_origin = %+v", empty.ByOrigin)
	}
}
