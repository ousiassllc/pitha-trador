package proposaltests

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

// The order-proposal read tools (issue #829) go through the same read-only
// connection: nothing they or a client can run writes, they leave every table
// they read as it was, and their statements name no secret or raw column. Two
// columns are read on purpose: order_proposal_events.detail_json (codes and
// numbers the application wrote) and order_proposals.limit_price (the
// proposal's own limit, not an order's).
func TestOrderProposalReadToolsUseOnlyReadStatements(t *testing.T) {
	path, w := testkit.NewMigratedDB(t)
	testkit.SeedProposalOutcomes(t, w)
	driverName, counter := testkit.NewCountingDriver(t)
	srv := newTestServer(t, paperresultsmcp.Options{Settings: enabledSettings(), DBPath: path, DriverName: driverName})
	tables := []string{"order_proposals", "order_proposal_events", "trade_signals", "paper_orders", "positions", "runtime_settings", "mcp_settings_audit"}
	before := map[string]int{}
	for _, table := range tables {
		before[table] = rowCount(t, w, table)
	}

	proposalPeriod := map[string]any{"from": "2026-10-01", "to": "2026-10-19"}
	srv.call(t, "order_proposals_list", proposalPeriod)
	srv.call(t, "order_proposal_summary", proposalPeriod)
	srv.call(t, "health_today", map[string]any{})
	for id := 1; id <= 6; id++ {
		srv.call(t, "order_proposal_get", map[string]any{"proposal_id": id})
	}

	toolStatements := counter.StatementLog() // before the write attempts below
	db, err := srv.DB()
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`UPDATE order_proposals SET status = 'filled'`,
		`DELETE FROM order_proposals`,
		`INSERT INTO order_proposal_events (proposal_id, at, to_status, actor) VALUES (1, 't', 'received', 'mcp')`,
		`UPDATE positions SET realized_pnl = 1`,
	} {
		if _, err := db.Exec(q); err == nil {
			t.Errorf("write succeeded on the MCP connection: %s", q)
		}
	}
	for _, table := range tables {
		if got := rowCount(t, w, table); got != before[table] {
			t.Errorf("%s: %d rows, was %d", table, got, before[table])
		}
	}

	statements := 0
	for _, stmt := range toolStatements {
		trimmed := strings.TrimSpace(stmt)
		if strings.HasPrefix(trimmed, "PRAGMA") || trimmed == "SELECT value FROM runtime_settings WHERE key = ?" || trimmed == "BEGIN" || trimmed == "COMMIT" || trimmed == "ROLLBACK" {
			continue
		}
		if !strings.HasPrefix(trimmed, "SELECT") {
			t.Errorf("a proposal read tool ran a non-SELECT statement: %s", testkit.FirstLine(stmt))
		}
		upper := strings.ToUpper(stmt)
		if strings.Contains(upper, "SELECT *") || strings.Contains(upper, ".*") {
			t.Errorf("SELECT * style statement: %s", testkit.FirstLine(stmt))
		}
		for _, f := range []string{"INSERT", "UPDATE ", "DELETE", "CREATE", "DROP", "ALTER", "secrets", "raw_data_json", "state_json",
			"response_json", "params_json", "features_json", "payload_json", "last_error", "encrypted_value", "current_price",
			"unrealized_pnl", "slippage_bps", "idempotency_key", "request_hash", "client"} {
			if strings.Contains(stmt, f) {
				t.Errorf("statement mentions %q: %s", f, testkit.FirstLine(stmt))
			}
		}
		if strings.Contains(stmt, "detail_json") && !strings.Contains(stmt, "FROM order_proposal_events") {
			t.Errorf("detail_json read outside order_proposal_events: %s", testkit.FirstLine(stmt))
		}
		if strings.Contains(stmt, "order_proposals") || strings.Contains(stmt, "order_proposal_events") {
			statements++
		}
	}
	if statements == 0 {
		t.Fatal("no statement over order_proposals was logged: the tools did not run")
	}
}

func rowCount(t *testing.T, w *sql.DB, table string) int {
	t.Helper()
	var n int
	if err := w.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
