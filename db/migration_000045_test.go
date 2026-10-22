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

// Migration 000045 (issue #860) takes a proposal's trade signal out of the Risk
// passes when it passed Risk but no order came of it. Rows of a proposal that
// ordered (filled, submitted), one still processing, a Policy Engine signal and
// a Risk rejection are not touched. The original risk_passed and reject_reason
// of every rewritten row are kept in trade_signal_rewrites_000045; the down
// migration restores exactly those values from it and drops the table.
func TestMigration000045_RewritesUnorderedProposalSignals(t *testing.T) {
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
	signal(1, "mcp-proposal-v1", 1, "legacy text") // rejected size_zero; a reason that must come back verbatim
	signal(2, "mcp-proposal-v1", 1, "")            // failed, no reason code recorded
	signal(3, "mcp-proposal-v1", 1, "")            // filled: has an order
	signal(4, "mcp-proposal-v1", 1, "")            // processing: recovery's
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

	// Retried proposals (issue #860): a retry linked a new signal to the proposal and left the old one risk_passed = 1 with no link.
	stamp := func(signalID int, at string) {
		mustExec(t, conn, `UPDATE trade_signals SET timestamp = '`+at+`' WHERE id = `+itoa(signalID))
	}
	retried := func(signalID int, status, at, waitedAt string) { // a proposal of instrument 1 that waited for the session at waitedAt
		proposal(0, status, "", signalID)
		mustExec(t, conn, `UPDATE order_proposals SET instrument_id = 1 WHERE id = `+itoa(proposals))
		stamp(signalID, at)
		mustExec(t, conn, `INSERT INTO order_proposal_events (proposal_id, at, from_status, to_status, reason_code, actor)
			VALUES (`+itoa(proposals)+`, '`+waitedAt+`', 'processing', 'waiting_session', 'outside_trading_session', 'app')`)
	}
	for id := 12; id <= 19; id++ {
		signal(id, "mcp-proposal-v1", 1, "")
	}
	retried(13, "filled", "2026-10-01T00:05:00.000Z", "2026-10-01T00:00:11.000Z") // 12 is its old signal: waited 1 s after it
	stamp(12, "2026-10-01T00:00:10.000Z")
	stamp(14, "2026-10-03T00:00:00.000Z") // an orphan nothing in the history points to
	stamp(15, "2026-10-02T00:00:10.000Z") // two proposals waited right after it: not one proposal
	retried(16, "expired", "2026-10-02T00:05:00.000Z", "2026-10-02T00:00:12.000Z")
	retried(17, "expired", "2026-10-02T00:06:00.000Z", "2026-10-02T00:00:13.000Z")
	stamp(18, "2026-10-04T00:00:00.000Z") // the only wait is 5 minutes later: outside the 60 s window
	retried(19, "filled", "2026-10-04T00:10:00.000Z", "2026-10-04T00:05:00.000Z")
	mustExec(t, conn, `INSERT INTO paper_orders (instrument_id, trade_signal_id, symbol, side, order_type, quantity, status, submitted_at)
		VALUES (1, 13, '7203', 'BUY', 'MARKET', 100, 'FILLED', 't'), (1, 19, '7203', 'BUY', 'MARKET', 100, 'FILLED', 't')`)

	snapshot := func() string {
		rows, err := conn.Query(`SELECT id, risk_passed, quote(reject_reason) FROM trade_signals ORDER BY id`)
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
	rewrites := func() string {
		rows, err := conn.Query(`SELECT signal_id, old_risk_passed, quote(old_reject_reason) FROM trade_signal_rewrites_000045 ORDER BY signal_id`)
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

	if err := m.Migrate(45); err != nil {
		t.Fatalf("migrate to 45: %v", err)
	}
	want := strings.Join([]string{
		"1|0|'proposal_rejected_after_risk: size_zero'",
		"2|0|'proposal_rejected_after_risk: unknown'",
		"3|1|NULL",
		"4|1|NULL",
		"5|0|'risk_engine_rejected: kill_switch_active'",
		"6|1|NULL",
		"7|0|'proposal_rejected_after_risk: outside_trading_session'",
		"8|0|'proposal_rejected_after_risk: recovered_after_restart'",
		"9|0|'proposal_rejected_after_risk: expired'",
		"10|0|'proposal_rejected_after_risk: cancel_requested'",
		"11|1|NULL", // an order came of it: still a Risk pass
		"12|0|'proposal_rejected_after_risk: outside_trading_session'", // an old signal of a retried proposal, traced by its history
		"13|1|NULL",
		"14|1|NULL", // orphans that cannot be traced to exactly one proposal stay as they are
		"15|1|NULL",
		"16|0|'proposal_rejected_after_risk: unknown'",
		"17|0|'proposal_rejected_after_risk: unknown'",
		"18|1|NULL",
		"19|1|NULL",
	}, "\n")
	if got := snapshot(); got != want {
		t.Fatalf("after up:\n%s\nwant:\n%s", got, want)
	}
	// Exactly the rewritten rows are kept, with their values from before.
	var link string
	var owner, ownerSignal int
	if err := conn.QueryRow(`SELECT w.link, w.proposal_id, op.trade_signal_id FROM trade_signal_rewrites_000045 w
		JOIN order_proposals op ON op.id = w.proposal_id WHERE w.signal_id = 12`).Scan(&link, &owner, &ownerSignal); err != nil {
		t.Fatal(err)
	}
	if link != "retry_history" || ownerSignal != 13 {
		t.Fatalf("signal 12 is bound by %q to proposal %d (signal %d), want retry_history and the proposal of signal 13", link, owner, ownerSignal)
	}
	if got, want := rewrites(), "1|1|'legacy text'\n2|1|NULL\n7|1|NULL\n8|1|NULL\n9|1|NULL\n10|1|NULL\n12|1|NULL\n16|1|NULL\n17|1|NULL"; got != want {
		t.Fatalf("trade_signal_rewrites_000045:\n%s\nwant:\n%s", got, want)
	}

	if err := m.Migrate(43); err != nil {
		t.Fatalf("migrate down to 43: %v", err)
	}
	if got := snapshot(); got != before {
		t.Fatalf("after down:\n%s\nwant:\n%s", got, before)
	}
	if err := conn.QueryRow(`SELECT 1 FROM trade_signal_rewrites_000045`).Scan(new(int)); err == nil {
		t.Fatal("trade_signal_rewrites_000045 still exists after down")
	}
}

func itoa(n int) string { return strconv.Itoa(n) }
