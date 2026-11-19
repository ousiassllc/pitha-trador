package virtualpapertests

import (
	"database/sql"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	repo "github.com/ousiassllc/pitha-trador/internal/repository/virtualpaper"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
)

type compareResult struct {
	From         string   `json:"from"`
	To           string   `json:"to"`
	Days         []string `json:"days"`
	ExcludedDays []struct {
		Day     string  `json:"day"`
		Missing []int64 `json:"missing_profile_ids"`
	} `json:"excluded_days"`
	Profiles []struct {
		ProfileID   int64   `json:"profile_id"`
		Name        string  `json:"name"`
		TradeCount  int     `json:"trade_count"`
		Unfinished  int     `json:"unfinished"`
		WinRate     float64 `json:"win_rate"`
		TotalPnLJPY float64 `json:"total_pnl_jpy"`
		MaxDD       float64 `json:"max_drawdown_pct"`
		Expectancy  struct {
			N     int     `json:"n"`
			Value float64 `json:"value"`
			HasCI bool    `json:"has_ci"`
		} `json:"expectancy_pct"`
		VsFirst *struct {
			Against int64 `json:"against_profile_id"`
			Diff    struct {
				Value float64 `json:"value"`
				N     int     `json:"n"`
			} `json:"daily_net_return_pct_diff"`
		} `json:"vs_first"`
	} `json:"profiles"`
	Caution string `json:"caution"`
}

// seedDay records one day with trades of the given net returns, the way the
// application's recorder does (the MCP never writes them).
func seedDay(t *testing.T, w *sql.DB, id int64, day string, nets ...float64) {
	t.Helper()
	var trades []repo.Trade
	for i, net := range nets {
		entry := at(day, 10, i)
		trades = append(trades, repo.Trade{
			InstrumentID: int64(i + 1), Symbol: "7203", Direction: domain.JevDirectionLong, EntryAt: entry, EntryPrice: 1000,
			ExitAt: entry.Add(5 * time.Minute), ExitPrice: 1000 * (1 + net/100), ExitReason: domain.ExitReasonTakeProfit, Quantity: 100, Net: net, PnLJPY: net * 100 * 1000 / 100,
		})
	}
	d := repo.Day{ProfileID: id, Day: day, Status: repo.DayDone, Bars: 10, DecisionsSeen: 1, Trades: len(trades)}
	if err := repo.RecordDay(t.Context(), w, d, trades, at(day, 17, 0)); err != nil {
		t.Fatal(err)
	}
}

func TestCompare(t *testing.T) {
	srv, w := newServer(t)
	a := srv.create(t, map[string]any{"name": "base"}).ID
	b := srv.create(t, map[string]any{"name": "tp1", "params": map[string]any{"take_profit_pct": 1}}).ID
	for _, day := range []string{"2026-10-12", "2026-10-13", "2026-10-14"} {
		seedDay(t, w, a, day, 0.5, -0.2)
	}
	seedDay(t, w, b, "2026-10-13", 0.3)
	seedDay(t, w, b, "2026-10-14", 0.4)
	// An unfinished trade of b on day 14 is counted but never enters a figure.
	if _, err := w.ExecContext(t.Context(), `INSERT INTO virtual_paper_trades (profile_id, day, instrument_id, symbol, direction, entry_at, entry_price, exit_at, exit_price, exit_reason, quantity,
gross_return_pct, slippage_return_pct, fee_return_pct, net_return_pct, pnl_jpy, created_at) VALUES (?, '2026-10-14', 9, '6758', 'LONG', 'e', 1, 'x', 1, 'data_ended', 1, 9, 9, 9, 9, 9, 't')`, b); err != nil {
		t.Fatal(err)
	}
	if _, err := w.ExecContext(t.Context(), `UPDATE virtual_paper_days SET trades = 2, unfinished = 1 WHERE profile_id = ? AND day = '2026-10-14'`, b); err != nil {
		t.Fatal(err)
	}

	var got compareResult
	srv.call(t, "virtual_paper_compare", map[string]any{"from": "2026-10-10", "to": "2026-10-17", "profile_ids": []int64{a, b}}).data(t, "virtual_paper_compare", &got)
	if len(got.Days) != 2 || got.Days[0] != "2026-10-13" || got.Days[1] != "2026-10-14" {
		t.Fatalf("days = %v, want the 2 days both profiles completed", got.Days)
	}
	if len(got.ExcludedDays) != 1 || got.ExcludedDays[0].Day != "2026-10-12" || len(got.ExcludedDays[0].Missing) != 1 || got.ExcludedDays[0].Missing[0] != b {
		t.Fatalf("excluded_days = %+v", got.ExcludedDays)
	}
	if len(got.Profiles) != 2 || got.Profiles[0].ProfileID != a || got.Profiles[0].TradeCount != 4 || got.Profiles[1].TradeCount != 2 || got.Profiles[1].Unfinished != 1 {
		t.Fatalf("profiles = %+v", got.Profiles)
	}
	base, tp := got.Profiles[0], got.Profiles[1]
	if base.WinRate != 0.5 || !near(base.Expectancy.Value, 0.15) || !base.Expectancy.HasCI || base.Expectancy.N != 4 || !near(base.TotalPnLJPY, 2*(0.5-0.2)*1000) || !near(base.MaxDD, 0.2) {
		t.Errorf("base = %+v", base)
	}
	if !near(tp.Expectancy.Value, 0.35) || tp.VsFirst == nil || tp.VsFirst.Against != a || tp.VsFirst.Diff.N != 2 || !near(tp.VsFirst.Diff.Value, 0.35-0.3) {
		t.Errorf("tp = %+v, paired %+v", tp, tp.VsFirst)
	}
	if !strings.Contains(got.Caution, "記録専用") {
		t.Errorf("caution = %q", got.Caution)
	}
}

func near(a, b float64) bool { d := a - b; return d < 1e-9 && d > -1e-9 }

func TestCompareDefaultsAndRefusals(t *testing.T) {
	srv, w := newServer(t)
	var empty compareResult
	srv.call(t, "virtual_paper_compare", map[string]any{}).data(t, "virtual_paper_compare", &empty)
	if empty.Profiles == nil || len(empty.Profiles) != 0 || len(empty.Days) != 0 {
		t.Fatalf("no data: %+v, want empty lists and no error", empty)
	}
	a := srv.create(t, map[string]any{"name": "a"}).ID
	b := srv.create(t, map[string]any{"name": "b"}).ID
	seedDay(t, w, a, "2026-10-13", 0.1)
	seedDay(t, w, b, "2026-10-13", 0.2)

	var all compareResult
	srv.call(t, "virtual_paper_compare", map[string]any{}).data(t, "virtual_paper_compare", &all)
	if len(all.Profiles) != 2 {
		t.Fatalf("default selection = %d profiles, want both that have data in the last 30 days", len(all.Profiles))
	}
	unprocessable := http.StatusUnprocessableEntity
	srv.call(t, "virtual_paper_compare", map[string]any{"profile_ids": []int64{a}}).wantError(t, unprocessable, paperresultsmcp.CodeInvalidArgument)
	srv.call(t, "virtual_paper_compare", map[string]any{"profile_ids": []int64{a, 999}}).wantError(t, unprocessable, paperresultsmcp.CodeInvalidArgument)
	srv.call(t, "virtual_paper_compare", map[string]any{"from": "2026-10-10"}).wantError(t, unprocessable, paperresultsmcp.CodeInvalidPeriod)
	srv.call(t, "virtual_paper_compare", map[string]any{"profile_ids": []int64{a, 0}}).wantError(t, unprocessable, paperresultsmcp.CodeInvalidArgument)
}
