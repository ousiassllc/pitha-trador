package mcptests

import (
	"database/sql"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

// A missing database file is db_unavailable and is not created; an empty
// (unmigrated) file is db_unavailable too, never migrated.
func TestDBUnavailable(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "pitha.db")
	srv := newTestServer(t, paperresultsmcp.Options{Settings: enabledSettings(), DBPath: missing})
	srv.call(t, "paper_daily_pnl", period).wantError(t, http.StatusServiceUnavailable, paperresultsmcp.CodeDBUnavailable)
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("a request created files: %v", entries)
	}

	empty := filepath.Join(t.TempDir(), "empty.db")
	ew, err := sql.Open("sqlite", empty)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ew.Exec(`CREATE TABLE unrelated (id INTEGER)`); err != nil {
		t.Fatal(err)
	}
	_ = ew.Close()
	srv2 := newTestServer(t, paperresultsmcp.Options{Settings: enabledSettings(), DBPath: empty})
	for _, tool := range eightTools {
		srv2.call(t, tool, period).wantError(t, http.StatusServiceUnavailable, paperresultsmcp.CodeDBUnavailable)
	}
	// The database that appears later is picked up (a failed open is not cached).
	path, w := testkit.NewMigratedDB(t)
	testkit.SeedPositions(t, w)
	srv3 := newTestServer(t, paperresultsmcp.Options{Settings: enabledSettings(), DBPath: path})
	var d perfData
	srv3.call(t, "paper_performance_summary", period).data(t, "paper_performance_summary", &d)
}

// The connection the MCP uses is read-only; across a full run of all eight
// tools no write, migration, checkpoint, journal-mode change or secrets /
// raw-column read is ever issued, and SELECT * is never used.
// boundedLastError is the only form in which a statement may name
// jobs.last_error: substr(last_error, 1, N).
var boundedLastError = regexp.MustCompile(`substr\(last_error, 1, ([0-9]+)\)`)

func TestMCPConnectionIsReadOnlyAndIssuesOnlyWhitelistedReads(t *testing.T) {
	path, w := testkit.NewMigratedDB(t)
	testkit.SeedAll(t, w)
	driverName, counter := testkit.NewCountingDriver(t)
	srv := newTestServer(t, paperresultsmcp.Options{Settings: enabledSettings(), DBPath: path, DriverName: driverName})
	for _, tool := range eightTools {
		srv.call(t, tool, period)
	}
	srv.call(t, healthToday, map[string]any{})

	db, err := srv.DB()
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO positions (instrument_id, entry_order_id, symbol, side, quantity, entry_price, current_price, opened_at)
			VALUES (1, 1, 'x', 'LONG', 1, 1, 1, 't')`,
		`UPDATE positions SET realized_pnl = 1`,
		`DELETE FROM positions`,
		`CREATE TABLE evil (id INTEGER)`,
		`INSERT INTO kill_switch_events (triggered_at, reason, detail_json) VALUES ('t', 'operator_manual', '{}')`,
	} {
		if _, err := db.Exec(q); err == nil {
			t.Errorf("write succeeded on the MCP connection: %s", testkit.FirstLine(q))
		}
	}
	var n int
	if err := w.QueryRow(`SELECT COUNT(*) FROM positions`).Scan(&n); err != nil || n != 8 {
		t.Fatalf("positions changed: %d, %v", n, err)
	}

	// runtime_settings is read by health_today through one statement (a fixed
	// key list, query.ReadableSettingKeys); any other mention stays forbidden.
	const settingRead = "SELECT value FROM runtime_settings WHERE key = ?"
	forbidden := []string{"wal_checkpoint", "journal_mode", "schema_migrations", "golang-migrate", "INSERT", "UPDATE ",
		"DELETE", "CREATE", "DROP", "ALTER", "ATTACH", "VACUUM", "secrets", "runtime_settings", "raw_data_json",
		"state_json", "response_json", "params_json", "features_json", "payload_json", "last_error", "detail_json",
		"encrypted_value", "current_price", "unrealized_pnl", "slippage_bps", "limit_price"}
	for _, stmt := range counter.StatementLog() {
		if strings.TrimSpace(stmt) == settingRead {
			continue
		}
		// jobs.last_error is read only through a bounded head (issue #801):
		// collection_status parses a fixed code token out of it in-process.
		// Any other mention of the column is still forbidden.
		stmt = boundedLastError.ReplaceAllStringFunc(stmt, func(m string) string {
			if n, _ := strconv.Atoi(boundedLastError.FindStringSubmatch(m)[1]); n > 256 {
				return m // too long a head stays flagged below
			}
			return ""
		})
		upper := strings.ToUpper(stmt)
		if strings.Contains(upper, "SELECT *") || strings.Contains(upper, ".*") {
			t.Errorf("SELECT * style statement: %s", testkit.FirstLine(stmt))
		}
		for _, f := range forbidden {
			if strings.Contains(stmt, f) && !strings.HasPrefix(strings.TrimSpace(stmt), "PRAGMA") {
				// the INSERT/UPDATE rows above are the test's own writes
				if strings.HasPrefix(strings.TrimSpace(stmt), "INSERT") || strings.HasPrefix(strings.TrimSpace(stmt), "UPDATE") ||
					strings.HasPrefix(strings.TrimSpace(stmt), "DELETE") || strings.HasPrefix(strings.TrimSpace(stmt), "CREATE") {
					continue
				}
				t.Errorf("statement mentions %q: %s", f, testkit.FirstLine(stmt))
			}
		}
	}
	// The only PRAGMA the MCP ever runs is the query_only check.
	for _, stmt := range counter.StatementLog() {
		if strings.HasPrefix(strings.TrimSpace(stmt), "PRAGMA") && strings.TrimSpace(stmt) != "PRAGMA query_only" {
			t.Errorf("unexpected PRAGMA: %s", stmt)
		}
	}
}
