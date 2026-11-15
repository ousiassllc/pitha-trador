package researchtests

import (
	"net/http"
	"strings"
	"testing"

	"fmt"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

func TestArgumentValidationIsStrict(t *testing.T) {
	path, _ := seed(t)
	srv := newTestServer(t, paperresultsmcp.Options{DBPath: path})
	for name, a := range map[string]map[string]any{
		"an unknown scope":                            args(map[string]any{"scope": "all"}),
		"an unknown field":                            args(map[string]any{"horizon": "30m"}),
		"a lower-case direction":                      args(map[string]any{"scope": "trader", "direction": "long"}),
		"NONE as a direction":                         args(map[string]any{"scope": "trader", "direction": "NONE"}),
		"a direction in the snapshots scope":          args(map[string]any{"direction": "LONG"}),
		"a direction in the explicit snapshots scope": args(map[string]any{"scope": "snapshots", "direction": "SHORT"}),
		"a wrongly typed scope":                       args(map[string]any{"scope": 1}),
	} {
		if r := srv.call(t, a); r.status != http.StatusUnprocessableEntity || !strings.Contains(string(r.body), "invalid_argument") {
			t.Errorf("%s: status %d body %s, want 422 invalid_argument", name, r.status, r.body)
		}
	}
	for name, a := range map[string]map[string]any{
		"only from":        {"from": "2026-10-10"},
		"a reversed range": {"from": "2026-10-18", "to": "2026-10-10"},
		"over 366 days":    {"from": "2025-01-01", "to": "2026-10-10"},
	} {
		if r := srv.call(t, a); r.status != http.StatusUnprocessableEntity || !strings.Contains(string(r.body), "invalid_period") {
			t.Errorf("%s: status %d body %s, want 422 invalid_period", name, r.status, r.body)
		}
	}
	// Valid: the explicit default scope and each decision scope.
	for _, a := range []map[string]any{args(map[string]any{"scope": "snapshots"}), args(map[string]any{"scope": "scout", "direction": "LONG"})} {
		if r := srv.call(t, a); r.status != http.StatusOK {
			t.Errorf("valid %v: status %d body %s", a, r.status, r.body)
		}
	}
}

func TestToolIsReadOnlyAndLeavesTheDatabaseUnchanged(t *testing.T) {
	path, w := seed(t)
	driverName, counter := testkit.NewCountingDriver(t)
	srv := newTestServer(t, paperresultsmcp.Options{DBPath: path, DriverName: driverName})
	dump := func() string {
		var b strings.Builder
		for _, table := range []string{"research_horizon_labels", "research_label_days", "research_label_day_stats", "market_snapshots", "jev_decisions", "calibration_outcomes", "counterfactual_labels", "runtime_settings"} {
			rows, err := w.Query(`SELECT * FROM ` + table + ` ORDER BY 1, 2, 3`)
			if err != nil {
				t.Fatal(err)
			}
			cols, _ := rows.Columns()
			for rows.Next() {
				vals := make([]any, len(cols))
				ptrs := make([]any, len(cols))
				for i := range vals {
					ptrs[i] = &vals[i]
				}
				if err := rows.Scan(ptrs...); err != nil {
					t.Fatal(err)
				}
				b.WriteString(table)
				for _, v := range vals {
					b.WriteString("|")
					b.WriteString(strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(toString(v), "\n", " "), "|", "/")))
				}
				b.WriteString("\n")
			}
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
			_ = rows.Close()
		}
		return b.String()
	}
	before := dump()
	for _, scope := range []string{"snapshots", "trader", "scout"} {
		if r := srv.call(t, args(map[string]any{"scope": scope})); r.status != http.StatusOK {
			t.Fatalf("%s: status %d body %s", scope, r.status, r.body)
		}
	}
	if after := dump(); after != before {
		t.Error("the tool changed the database")
	}

	// The MCP connection refuses a write, and the tool's statements are reads of the research tables,
	// market_snapshots (counts) and jev_decisions (identity columns) only.
	db, err := srv.DB()
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO research_horizon_labels (snapshot_id, instrument_id, symbol, snapshot_at, horizon, exit_at, entry_price, exit_price, long_gross_bps, long_cost_bps, long_net_bps, label_version)
			VALUES (99, 1, 'x', 't', '30m', 't', 1, 1, 1, 1, 0, 1)`,
		`UPDATE research_horizon_labels SET long_net_bps = 0`,
		`DELETE FROM research_label_days`,
	} {
		if _, err := db.Exec(q); err == nil {
			t.Errorf("write succeeded on the MCP connection: %s", testkit.FirstLine(q))
		}
	}
	forbidden := []string{"INSERT", "UPDATE ", "DELETE", "CREATE", "DROP", "ALTER", "ATTACH", "VACUUM", "SELECT *", ".*",
		"raw_data_json", "state_json", "response_json", "runtime_settings", "secrets", "payload_json", "calibration_outcomes", "counterfactual_labels", "calibration_outcome_origins"}
	seen := 0
	for _, stmt := range counter.StatementLog() {
		if !strings.Contains(stmt, "research_") && !strings.Contains(stmt, "market_snapshots") && !strings.Contains(stmt, "jev_decisions") {
			continue
		}
		if strings.Contains(stmt, "INSERT INTO research_horizon_labels") || strings.Contains(stmt, "UPDATE research_horizon_labels") || strings.Contains(stmt, "DELETE FROM research_label_days") {
			continue // the refused writes above
		}
		seen++
		upper := strings.ToUpper(stmt)
		for _, f := range forbidden {
			if strings.Contains(upper, strings.ToUpper(f)) {
				t.Errorf("statement names %q: %s", f, testkit.FirstLine(stmt))
			}
		}
	}
	if seen == 0 {
		t.Fatal("no statement of the tool was observed")
	}
}

func toString(v any) string {
	switch x := v.(type) {
	case nil:
		return "NULL"
	case []byte:
		return string(x)
	default:
		return strings.TrimSpace(strings.Join(strings.Fields(sprint(x)), " "))
	}
}

func sprint(v any) string { return fmt.Sprint(v) }
