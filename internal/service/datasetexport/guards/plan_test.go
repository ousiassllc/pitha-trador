package guards_test

import (
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/service/datasetexport"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

func TestMain(m *testing.M) {
	code := m.Run()
	testkit.RemoveTemplate()
	os.Exit(code)
}

// bigTables are the tables whose full scan would blow the call's 3 s budget.
var bigTables = []string{
	"jev_decisions", "market_snapshots", "trade_signals", "calibration_outcomes", "research_horizon_labels",
	"counterfactual_labels", "jev_shadow_decisions", "jev_trial_decisions",
}

// Every statement the export issues reaches the big tables through an index (a SEARCH), never a
// full SCAN: the reads stay bounded by the period however large the tables grow.
func TestEveryStatementUsesAnIndex(t *testing.T) {
	driverName, counter := testkit.NewCountingDriver(t)
	path, w := testkit.NewMigratedDB(t)
	testkit.MustExec(t, w, `INSERT INTO runtime_settings (key, value, updated_at) VALUES ('research.export.enabled', '1', 't')`)
	testkit.MustExec(t, w, `INSERT INTO instruments (id, symbol, name, market) VALUES (1, '7203', 'a', 'TSE')`)
	testkit.MustExec(t, w, `INSERT INTO jev_decisions (id, instrument_id, symbol, timestamp, decision_type, state_hash, state_json, question_version, response_json, direction, confidence, latency_ms, model_id, created_at)
		VALUES (1, 1, 'S', '2026-10-05T01:00:00.000000000Z', 'trader', 'h', '{}', 'v', '{}', 'LONG', 0.5, 5, 'm', '2026-10-05T01:00:00.000000000Z')`)
	testkit.MustExec(t, w, `INSERT INTO jev_shadow_decisions (jev_decision_id, prompt_version, model_id, response_json, direction, regime, entry_quality, confidence, toxic_flow, liquidity_stressed, continuation_probability, latency_ms)
		VALUES (1, 'a', 'm', '{}', 'LONG', 'RANGE', 'fair', 0.5, 0.1, 0.1, 0.5, 5)`)
	testkit.MustExec(t, w, `INSERT INTO jev_trial_decisions (jev_decision_id, arm_id, prompt_version, requested_model, status, direction, regime, entry_quality, confidence, toxic_flow, liquidity_stressed, continuation_probability, cost_source)
		VALUES (1, 'x', 'v', 'req', 'ok', 'LONG', 'TREND', 'strong', 0.8, 0.1, 0.1, 0.9, 'unknown')`)

	db, err := sql.Open(driverName, sqlitedb.FileURI(path, "mode=ro&_pragma=query_only(ON)"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	from := time.Date(2026, 10, 5, 0, 0, 0, 0, query.JST)
	start := len(counter.StatementLog())
	resp, _, err := datasetexport.Run(t.Context(), db, datasetexport.Params{From: from, To: from.AddDate(0, 0, 7), Now: testkit.FixedNow()})
	if err != nil || resp.Manifest == nil {
		t.Fatalf("export: %+v %v", resp, err)
	}
	checked := 0
	for _, stmt := range counter.StatementLog()[start:] {
		if !strings.Contains(stmt, "jev_decisions") {
			continue
		}
		args := make([]any, strings.Count(stmt, "?"))
		for i := range args {
			args[i] = 2
		}
		args[0], args[1] = "2026-10-04T15:00:00.000000000Z", "2026-10-11T15:00:00.000000000Z"
		rows, err := db.QueryContext(t.Context(), "EXPLAIN QUERY PLAN "+stmt, args...)
		if err != nil {
			t.Fatalf("plan of %s: %v", firstLine(stmt), err)
		}
		for rows.Next() {
			var id, parent, unused int
			var detail string
			if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
				t.Fatal(err)
			}
			t.Logf("%.40s | %s", firstLine(stmt), detail)
			for _, table := range bigTables {
				if strings.HasPrefix(detail, "SCAN "+table) || strings.HasPrefix(detail, "SCAN "+alias(stmt, table)+" ") && alias(stmt, table) != "" {
					t.Errorf("%s: full scan (%s)", firstLine(stmt), detail)
				}
			}
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		_ = rows.Close()
		checked++
	}
	if checked < 7 {
		t.Fatalf("only %d statements checked", checked)
	}
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// alias returns the alias a statement gives table ("d" for "jev_decisions d"), "" when it has none.
func alias(stmt, table string) string {
	for _, kw := range []string{"FROM ", "JOIN "} {
		for rest := stmt; ; {
			i := strings.Index(rest, kw+table+" ")
			if i < 0 {
				break
			}
			rest = rest[i+len(kw+table)+1:]
			if f := strings.Fields(rest); len(f) > 0 && len(f[0]) <= 2 {
				return f[0]
			}
		}
	}
	return ""
}
