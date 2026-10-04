package sqlitedb_test

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	migratesqlite "github.com/golang-migrate/migrate/v4/database/sqlite"
	"github.com/golang-migrate/migrate/v4/source/iofs"

	"github.com/ousiassllc/pitha-trador/db"
)

// Migration 000022 must drop the jev_decision_vectors built before the
// bid/ask swap fix (#458: sign-inverted spread/imbalance) so RAG similarity
// search can no longer return those decisions, while keeping the
// jev_decisions audit rows and leaving later vectors searchable (issue #464).
func TestMigration000022_PurgesPreFixDecisionVectorsKeepsDecisions(t *testing.T) {
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
	if err := m.Migrate(21); err != nil {
		t.Fatalf("migrate to 21: %v", err)
	}

	const vec = `'[0,0,0,0,0,0,0,0,0,0,0,0,0,0]'`
	for _, stmt := range []string{
		`INSERT INTO instruments (symbol, name, market, is_active) VALUES ('7203', 'T', 'TSE Prime', 1)`,
		`INSERT INTO jev_decisions (instrument_id, symbol, timestamp, decision_type, state_hash, state_json, question_version, response_json, latency_ms, model_id)
		 VALUES (1, '7203', '2026-10-04T01:00:00.000000000Z', 'scout', 'h', '{"best_bid":1}', 'scout-v3', '{}', 1, 'm')`,
		`INSERT INTO jev_decision_vectors (decision_id, embedding) VALUES (1, ` + vec + `)`,
		`INSERT INTO market_snapshot_vectors (snapshot_id, embedding) VALUES (1, ` + vec + `)`,
	} {
		if _, err := conn.Exec(stmt); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	if err := m.Migrate(22); err != nil {
		t.Fatalf("migrate to 22: %v", err)
	}

	count := func(query string) int {
		t.Helper()
		var n int
		if err := conn.QueryRow(query).Scan(&n); err != nil {
			t.Fatalf("%s: %v", query, err)
		}
		return n
	}
	if n := count(`SELECT COUNT(*) FROM jev_decision_vectors`); n != 0 {
		t.Errorf("jev_decision_vectors rows = %d, want 0 (pre-fix vectors purged)", n)
	}
	if n := count(`SELECT COUNT(*) FROM (SELECT decision_id FROM jev_decision_vectors WHERE embedding MATCH ` + vec + ` ORDER BY distance LIMIT 5)`); n != 0 {
		t.Errorf("similarity search over purged vectors returned %d hits, want 0", n)
	}
	if n := count(`SELECT COUNT(*) FROM jev_decisions`); n != 1 {
		t.Errorf("jev_decisions rows = %d, want the audit row untouched", n)
	}
	if n := count(`SELECT COUNT(*) FROM market_snapshot_vectors`); n != 1 {
		t.Errorf("market_snapshot_vectors rows = %d, want untouched (retention purges them)", n)
	}

	// New decisions are indexed and searchable after the migration.
	if _, err := conn.Exec(`INSERT INTO jev_decision_vectors (decision_id, embedding) VALUES (2, ` + vec + `)`); err != nil {
		t.Fatalf("index post-fix vector: %v", err)
	}
	if n := count(`SELECT COUNT(*) FROM (SELECT decision_id FROM jev_decision_vectors WHERE embedding MATCH ` + vec + ` ORDER BY distance LIMIT 5)`); n != 1 {
		t.Errorf("post-fix vector hits = %d, want 1", n)
	}

	if err := m.Migrate(21); err != nil {
		t.Fatalf("migrate down to 21: %v", err)
	}
}
