package mcptests

import (
	"net/http"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

// Period rules (API spec §1): 366 days max, a default of the last 30 JST
// days, one-sided or malformed periods and a future `to` are invalid_period.
func TestPeriodValidation(t *testing.T) {
	srv := seededServer(t, nil)
	bad := []map[string]any{
		{"from": "2025-10-01", "to": "2026-10-03"}, // 367 days
		{"from": "2026-10-01"},                     // one side only
		{"to": "2026-10-10"},
		{"from": "2026-10-10", "to": "2026-10-10"}, // to <= from
		{"from": "2026-10-10", "to": "2026-10-01"},
		{"from": "2026-10-17", "to": "2026-10-20"}, // beyond JST tomorrow (10-19)
		{"from": "2026/10/01", "to": "2026-10-10"},
		{"from": "20261001", "to": "2026-10-10"},
		{"from": "2026-1-1", "to": "2026-10-10"},
		{"from": "2026-13-01", "to": "2026-10-10"},
		{"from": "", "to": "2026-10-10"},
	}
	for _, name := range eightTools {
		for _, a := range bad {
			srv.call(t, name, a).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidPeriod)
		}
	}
	// Exactly 366 days and a `to` of JST tomorrow are accepted.
	for _, a := range []map[string]any{
		{"from": "2025-10-01", "to": "2026-10-02"},
		{"from": "2026-10-18", "to": "2026-10-19"},
	} {
		var d perfData
		srv.call(t, "paper_performance_summary", a).data(t, "paper_performance_summary", &d)
	}
}

// With no period the window is the last 30 JST days including today:
// [2026-09-19 00:00 JST, 2026-10-19 00:00 JST) for fixedNow (10-18 JST).
func TestDefaultPeriodIsLast30Days(t *testing.T) {
	path, w := testkit.NewMigratedDB(t)
	testkit.MustExec(t, w, `INSERT INTO instruments (id, symbol, name, market) VALUES (1, '7203', 'n', 'TSE')`)
	for _, c := range []string{
		"2026-09-18T14:59:59Z", // JST 09-18 23:59:59: just before the window
		"2026-09-18T15:00:00Z", // JST 09-19 00:00:00: first instant inside
		"2026-10-18T14:59:59Z", // JST 10-18 23:59:59: last instant inside
		"2026-10-18T15:00:00Z", // JST 10-19 00:00:00: excluded (half open)
	} {
		testkit.Position(t, w, c, new(100.0), 1000, 100, nil, 99, nil)
	}
	srv := newTestServer(t, paperresultsmcp.Options{Settings: enabledSettings(), DBPath: path})
	var d perfData
	srv.call(t, "paper_performance_summary", map[string]any{}).data(t, "paper_performance_summary", &d)
	if d.ClosedTradeCount != 2 {
		t.Fatalf("default period counted %d trades, want 2", d.ClosedTradeCount)
	}
}

// List limits: default 50, max 200 (limit_exceeded above), metalabel max 50.
func TestListLimits(t *testing.T) {
	path, w := testkit.NewMigratedDB(t)
	testkit.MustExec(t, w, `INSERT INTO instruments (id, symbol, name, market) VALUES (1, '7203', 'n', 'TSE')`)
	testkit.MustExec(t, w, `WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < 260)
		INSERT INTO positions (instrument_id, entry_order_id, symbol, side, quantity, entry_price, current_price,
			realized_pnl, opened_at, closed_at)
		SELECT 1, 1, '7203', 'LONG', 1, 100, 100, 1, '2026-10-01T00:00:00.000000000Z',
			strftime('%Y-%m-%dT%H:%M:%S', '2026-10-01 00:00:00', '+' || i || ' seconds') || '.000000000Z' FROM n`)
	testkit.MustExec(t, w, `WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < 60)
		INSERT INTO kill_switch_events (triggered_at, reason, detail_json)
		SELECT strftime('%Y-%m-%dT%H:%M:%S', '2026-10-01 00:00:00', '+' || i || ' seconds') || '.000000000Z',
			'operator_manual', '{}' FROM n`)
	testkit.MustExec(t, w, `WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < 60)
		INSERT INTO metalabel_models (version, algorithm, label_version, status, trained_at, train_start, train_end,
			oos_start, oos_end, train_samples, oos_samples, feature_names_json, params_json, created_at)
		SELECT 'v' || i, 'a', 2, 'active',
			strftime('%Y-%m-%dT%H:%M:%S', '2026-10-01 00:00:00', '+' || i || ' seconds') || '.000000000Z',
			't', 't', 't', 't', 1, 1, '[]', '{}', 't' FROM n`)
	srv := newTestServer(t, paperresultsmcp.Options{Settings: enabledSettings(), DBPath: path})

	count := func(tool string, a map[string]any, key string) int {
		var d map[string]any
		srv.call(t, tool, a).data(t, tool, &d)
		list, _ := d[key].([]any)
		return len(list)
	}
	if n := count("paper_trades", period, "trades"); n != 50 {
		t.Fatalf("default trades limit = %d, want 50", n)
	}
	if n := count("paper_trades", args(map[string]any{"limit": 200}), "trades"); n != 200 {
		t.Fatalf("trades limit 200 = %d", n)
	}
	srv.call(t, "paper_trades", args(map[string]any{"limit": 201})).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeLimitExceeded)
	if n := count("kill_switch_history", period, "events"); n != 50 {
		t.Fatalf("default kill switch limit = %d, want 50", n)
	}
	srv.call(t, "kill_switch_history", args(map[string]any{"limit": 201})).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeLimitExceeded)
	if n := count("metalabel_history", period, "models"); n != 50 {
		t.Fatalf("default metalabel limit = %d, want 50", n)
	}
	srv.call(t, "metalabel_history", args(map[string]any{"limit": 51})).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeLimitExceeded)
}

// 5,000 closed positions in the period still aggregate; 5,001 is
// limit_exceeded for every aggregate over closed positions, with no partial
// data in the response.
func TestScanLimit5000(t *testing.T) {
	path, w := testkit.NewMigratedDB(t)
	testkit.MustExec(t, w, `INSERT INTO instruments (id, symbol, name, market) VALUES (1, '7203', 'n', 'TSE')`)
	insert := func(n int) {
		testkit.MustExec(t, w, `WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < ?)
			INSERT INTO positions (instrument_id, entry_order_id, symbol, side, quantity, entry_price, current_price,
				realized_pnl, opened_at, closed_at)
			SELECT 1, 1, '7203', 'LONG', 1, 100, 100, 1, '2026-10-01T00:00:00.000000000Z',
				strftime('%Y-%m-%dT%H:%M:%S', '2026-10-01 00:00:00', '+' || i || ' seconds') || '.000000000Z' FROM n`, n)
	}
	insert(5000)
	srv := newTestServer(t, paperresultsmcp.Options{Settings: enabledSettings(), DBPath: path})
	var d perfData
	srv.call(t, "paper_performance_summary", period).data(t, "paper_performance_summary", &d)
	if d.ClosedTradeCount != 5000 || d.TotalPnlJPY != 5000 {
		t.Fatalf("5000 rows: %+v", d)
	}

	testkit.MustExec(t, w, `INSERT INTO positions (instrument_id, entry_order_id, symbol, side, quantity, entry_price,
		current_price, realized_pnl, opened_at, closed_at)
		VALUES (1, 1, '7203', 'LONG', 1, 100, 100, 1, 'x', '2026-10-02T00:00:00.000000000Z')`)
	for _, tool := range []string{"paper_performance_summary", "paper_daily_pnl"} {
		r := srv.call(t, tool, period)
		r.wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeLimitExceeded)
		if strings.Contains(string(r.body), `"result"`) || strings.Contains(string(r.body), "closed_trade_count") ||
			strings.Contains(string(r.body), "structuredContent") {
			t.Fatalf("%s: partial result in error body: %s", tool, r.body)
		}
	}
}

// Fixed spec constants.
