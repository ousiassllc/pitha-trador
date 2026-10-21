package db_test

import (
	"database/sql"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	migratesqlite "github.com/golang-migrate/migrate/v4/database/sqlite"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "modernc.org/sqlite"

	"github.com/ousiassllc/pitha-trador/db"
)

// Migration 000046 (issue #860) takes a proposal's trade signal out of the Risk
// passes when it passed Risk but no order came of it. Rows of a proposal that
// ordered (filled, submitted), one still processing, a Policy Engine signal and
// a Risk rejection are not touched; the down migration puts the rewritten rows
// back.
func TestMigration000046_RewritesUnorderedProposalSignals(t *testing.T) {
	conn, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "m.db")+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	src, err := iofs.New(db.MigrationsFS, "migrations")
	if err != nil {
		t.Fatalf("iofs: %v", err)
	}
	drv, err := migratesqlite.WithInstance(conn, &migratesqlite.Config{})
	if err != nil {
		t.Fatalf("driver: %v", err)
	}
	m, err := migrate.NewWithInstance("iofs", src, "sqlite", drv)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := m.Migrate(43); err != nil {
		t.Fatalf("migrate to 43: %v", err)
	}

	mustExec(t, conn, `INSERT INTO instruments (id, symbol, name, market) VALUES (1, '7203', 'n', 'TSE')`)
	signal := func(id int, version string, passed int, reason string) {
		r := "NULL"
		if reason != "" {
			r = "'" + reason + "'"
		}
		mustExec(t, conn, `INSERT INTO trade_signals (id, instrument_id, symbol, timestamp, direction, policy_version, risk_passed, reject_reason)
			VALUES (`+itoa(id)+`, 1, '7203', 't', 'LONG', '`+version+`', `+itoa(passed)+`, `+r+`)`)
	}
	proposals := 0 // order_proposals ids are assigned in order
	proposal := func(_ int, status, statusReason string, signalID int) {
		proposals++
		id := proposals
		mustExec(t, conn, strings.Replace(insertProposalSQL, "'key-00000001'", "'key-"+strings.Repeat("0", 8-len(itoa(id)))+itoa(id)+"'", 1))
		mustExec(t, conn, `UPDATE order_proposals SET status = '`+status+`', status_reason = '`+statusReason+`', trade_signal_id = `+itoa(signalID)+` WHERE id = `+itoa(id))
	}
	signal(1, "mcp-proposal-v1", 1, "") // rejected size_zero
	signal(2, "mcp-proposal-v1", 1, "") // failed, no reason code recorded
	signal(3, "mcp-proposal-v1", 1, "") // filled: has an order
	signal(4, "mcp-proposal-v1", 1, "") // processing: recovery's
	signal(5, "mcp-proposal-v1", 0, "risk_engine_rejected: kill_switch_active")
	signal(6, "policy-v1", 1, "") // the Policy Engine's
	for id := 7; id <= 11; id++ {
		signal(id, "mcp-proposal-v1", 1, "")
	}
	proposal(1, "rejected", "size_zero", 1)
	proposal(2, "failed", "", 2)
	proposal(3, "filled", "", 3)
	proposal(4, "processing", "", 4)
	proposal(5, "rejected", "kill_switch_active", 5)
	proposal(7, "waiting_session", "outside_trading_session", 7)
	proposal(8, "received", "recovered_after_restart", 8)
	proposal(9, "expired", "expired", 9)
	proposal(10, "cancelled", "cancel_requested", 10)
	proposal(11, "cancelled", "cancel_requested", 11) // its limit order was placed, then cancelled
	mustExec(t, conn, `INSERT INTO paper_orders (instrument_id, trade_signal_id, symbol, side, order_type, quantity, status, submitted_at)
		VALUES (1, 3, '7203', 'BUY', 'MARKET', 100, 'FILLED', 't'), (1, 11, '7203', 'BUY', 'LIMIT', 100, 'CANCELLED', 't')`)

	snapshot := func() string {
		rows, err := conn.Query(`SELECT id, risk_passed, COALESCE(reject_reason, '-') FROM trade_signals ORDER BY id`)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = rows.Close() }()
		var out []string
		for rows.Next() {
			var id, passed int
			var reason string
			if err := rows.Scan(&id, &passed, &reason); err != nil {
				t.Fatal(err)
			}
			out = append(out, itoa(id)+"|"+itoa(passed)+"|"+reason)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		return strings.Join(out, "\n")
	}
	before := snapshot()

	if err := m.Migrate(46); err != nil {
		t.Fatalf("migrate to 46: %v", err)
	}
	want := strings.Join([]string{
		"1|0|proposal_rejected_after_risk: size_zero",
		"2|0|proposal_rejected_after_risk: unknown",
		"3|1|-",
		"4|1|-",
		"5|0|risk_engine_rejected: kill_switch_active",
		"6|1|-",
		"7|0|proposal_rejected_after_risk: outside_trading_session",
		"8|0|proposal_rejected_after_risk: recovered_after_restart",
		"9|0|proposal_rejected_after_risk: expired",
		"10|0|proposal_rejected_after_risk: cancel_requested",
		"11|1|-", // an order came of it: still a Risk pass
	}, "\n")
	if got := snapshot(); got != want {
		t.Fatalf("after up:\n%s\nwant:\n%s", got, want)
	}

	if err := m.Migrate(43); err != nil {
		t.Fatalf("migrate down to 43: %v", err)
	}
	if got := snapshot(); got != before {
		t.Fatalf("after down:\n%s\nwant:\n%s", got, before)
	}
}

func itoa(n int) string { return strconv.Itoa(n) }
