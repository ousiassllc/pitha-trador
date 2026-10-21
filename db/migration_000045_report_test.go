package db_test

import (
	"bytes"
	"database/sql"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	migratesqlite "github.com/golang-migrate/migrate/v4/database/sqlite"
	"github.com/golang-migrate/migrate/v4/source/iofs"

	"github.com/ousiassllc/pitha-trador/db"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
)

func captureSlog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

// The start that applies migration 000045 logs how many trade_signals it
// rewrote (the rows of its backup table); the next start, which applies
// nothing, logs nothing, so the line never repeats (issue #860).
func TestOpen_ReportsHowManyTradeSignalsMigration000045Rewrote(t *testing.T) {
	path := filepath.Join(t.TempDir(), "m.db")
	conn, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	src, err := iofs.New(db.MigrationsFS, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	drv, err := migratesqlite.WithInstance(conn, &migratesqlite.Config{})
	if err != nil {
		t.Fatal(err)
	}
	m, err := migrate.NewWithInstance("iofs", src, "sqlite", drv)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Migrate(43); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO instruments (id, symbol, name, market) VALUES (1, '7203', 'n', 'TSE')`,
		`INSERT INTO trade_signals (id, instrument_id, symbol, timestamp, direction, policy_version, risk_passed) VALUES
			(1, 1, '7203', 't', 'LONG', 'mcp-proposal-v1', 1), (2, 1, '7203', 't', 'LONG', 'mcp-proposal-v1', 1), (3, 1, '7203', 't', 'LONG', 'mcp-proposal-v1', 1)`,
	} {
		if _, err := conn.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	for id, status := range map[int]string{1: "rejected", 2: "expired", 3: "filled"} {
		key := "key-0000000" + string(rune('0'+id))
		if _, err := conn.Exec(`INSERT INTO order_proposals (idempotency_key, request_hash, received_at, symbol, bucket, action, quantity, order_type,
			stop_loss_price, exit_plan, max_loss_jpy, rationale, sources_json, expires_at, updated_at, status, status_reason, trade_signal_id)
			VALUES (?, 'h', 't', '7203', 'stock', 'open_long', 100, 'MARKET', 900, 'plan', 10000, 'why', '[]', 't', 't', ?, '', ?)`, key, status, id); err != nil {
			t.Fatal(err)
		}
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}

	logs := captureSlog(t)
	opened, err := sqlitedb.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "migration 000045: rewrote 2 trade_signals (退避テーブル trade_signal_rewrites_000045)"
	if !strings.Contains(logs.String(), want) {
		t.Fatalf("log = %s\nwant a line with %q", logs.String(), want)
	}
	t.Logf("log line: %s", strings.TrimSpace(logs.String()))
	if err := opened.Close(); err != nil {
		t.Fatal(err)
	}

	logs.Reset()
	again, err := sqlitedb.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = again.Close() })
	if strings.Contains(logs.String(), "migration 000045") {
		t.Fatalf("the second start logged the migration again: %s", logs.String())
	}
}
