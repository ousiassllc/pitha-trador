package actuals_test

import (
	"database/sql"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/repository/actuals/actualstest"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
)

// verifyQueries splits tools/verify-queries.sql at its "-- name: Qn ..."
// markers into name -> statement (name is the leading "Qn").
func verifyQueries(t *testing.T) (names []string, queries map[string]string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "tools", "verify-queries.sql"))
	if err != nil {
		t.Fatalf("read tools/verify-queries.sql: %v", err)
	}
	queries = map[string]string{}
	current := ""
	for _, line := range strings.Split(string(raw), "\n") {
		if rest, ok := strings.CutPrefix(line, "-- name: "); ok {
			current = strings.Fields(rest)[0]
			names = append(names, current)
			continue
		}
		if current != "" {
			queries[current] += line + "\n"
		}
	}
	return names, queries
}

type row map[string]any

func run(t *testing.T, db *sql.DB, name, query string) []row {
	t.Helper()
	rows, err := db.Query(query)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	defer func() { _ = rows.Close() }()
	cols, _ := rows.Columns()
	var out []row
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatalf("%s: scan: %v", name, err)
		}
		r := row{}
		for i, c := range cols {
			if b, ok := vals[i].([]byte); ok {
				vals[i] = string(b)
			}
			r[c] = vals[i]
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return out
}

func num(v any) (float64, bool) {
	switch x := v.(type) {
	case int64:
		return float64(x), true
	case float64:
		return x, true
	}
	return 0, false
}

// find returns the row whose columns match every key/value of where.
func find(rows []row, where map[string]any) row {
	for _, r := range rows {
		ok := true
		for k, want := range where {
			if fmt.Sprint(r[k]) != fmt.Sprint(want) {
				ok = false
			}
		}
		if ok {
			return r
		}
	}
	return nil
}

func expect(t *testing.T, name string, r row, want map[string]any) {
	t.Helper()
	if r == nil {
		t.Errorf("%s: row not found", name)
		return
	}
	for col, w := range want {
		got := r[col]
		if w == nil {
			if got != nil {
				t.Errorf("%s.%s = %v, want NULL", name, col, got)
			}
			continue
		}
		if wf, ok := w.(float64); ok {
			g, isNum := num(got)
			if !isNum || math.Abs(g-wf) > 0.005 { // the queries ROUND to 2-4 digits
				t.Errorf("%s.%s = %v, want %v", name, col, got, wf)
			}
			continue
		}
		if fmt.Sprint(got) != fmt.Sprint(w) {
			t.Errorf("%s.%s = %v, want %v", name, col, got, w)
		}
	}
}

// Every query of tools/verify-queries.sql runs against an empty database
// without a syntax or column-name error, and writes nothing.
func TestVerifyQueries_EmptyDatabase(t *testing.T) {
	names, queries := verifyQueries(t)
	if len(names) != 10 {
		t.Fatalf("found queries %v, want Q0..Q9", names)
	}
	db := openDB(t)
	for _, name := range names {
		q := queries[name]
		if first := strings.ToUpper(firstStatementWord(q)); first != "SELECT" && first != "WITH" {
			t.Errorf("%s starts with %q: queries must be read-only SELECTs", name, first)
		}
		run(t, db, name, q)
	}
}

func firstStatementWord(q string) string {
	for _, line := range strings.Split(q, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "--") {
			continue
		}
		return strings.Fields(line)[0]
	}
	return ""
}

// On the synthetic database the queries reproduce the hand-computed figures
// documented on actualstest (the same ones internal/service/actuals checks).
func TestVerifyQueries_SyntheticDatabase(t *testing.T) {
	db := openDB(t)
	actualstest.Seed(t, db)
	_, queries := verifyQueries(t)
	q := func(name string) []row { return run(t, db, name, queries[name]) }

	q0 := q("Q0")
	expect(t, "Q0 closed", find(q0, map[string]any{"item": "closed_positions"}), map[string]any{"n": 6.0, "first_day": "2026-10-12", "last_day": "2026-10-14", "days": 3.0})
	expect(t, "Q0 open", find(q0, map[string]any{"item": "open_positions"}), map[string]any{"n": 1.0})
	expect(t, "Q0 manual", find(q0, map[string]any{"item": "closed_without_signal_link"}), map[string]any{"n": 1.0})
	expect(t, "Q0 v1", find(q0, map[string]any{"item": "outcomes_label_version_1"}), map[string]any{"n": 3.0})
	expect(t, "Q0 v2", find(q0, map[string]any{"item": "outcomes_label_version_2"}), map[string]any{"n": 6.0})

	q1 := q("Q1")
	if len(q1) != 1 {
		t.Fatalf("Q1 rows = %v", q1)
	}
	expect(t, "Q1", q1[0], map[string]any{
		"n": 6.0, "days": 3.0, "wins": 3.0, "win_rate": 0.5, "net_pnl_jpy": -1000.0,
		"ev_jpy": -1000.0 / 6, "ev_bps": -100.0 / 6, "fees_jpy": 100.0, "profit_factor": 1040.0 / 2040,
	})

	q2 := q("Q2")
	expect(t, "Q2 day", find(q2, map[string]any{"dim": "day", "key": "2026-10-12"}), map[string]any{"n": 2.0, "net_pnl_jpy": -440.0})
	expect(t, "Q2 week", find(q2, map[string]any{"dim": "week"}), map[string]any{"n": 6.0, "net_pnl_jpy": -1000.0})
	expect(t, "Q2 LONG", find(q2, map[string]any{"dim": "direction", "key": "LONG"}), map[string]any{"n": 5.0, "net_pnl_jpy": 20.0, "win_rate": 0.6})
	expect(t, "Q2 SHORT", find(q2, map[string]any{"dim": "direction", "key": "SHORT"}), map[string]any{"n": 1.0, "net_pnl_jpy": -1020.0})
	expect(t, "Q2 stop_loss", find(q2, map[string]any{"dim": "exit_reason", "key": "stop_loss"}), map[string]any{"n": 2.0, "net_pnl_jpy": -2040.0})
	expect(t, "Q2 momentum", find(q2, map[string]any{"dim": "ab_arm", "key": "momentum"}), map[string]any{"n": 3.0, "net_pnl_jpy": 1040.0})
	expect(t, "Q2 no arm", find(q2, map[string]any{"dim": "ab_arm", "key": ""}), map[string]any{"n": 2.0, "net_pnl_jpy": -1020.0})
	expect(t, "Q2 v-a", find(q2, map[string]any{"dim": "meta_model", "key": "v-a"}), map[string]any{"n": 4.0, "net_pnl_jpy": 20.0})

	q3 := q("Q3")
	expect(t, "Q3", q3[0], map[string]any{"n": 6.0, "max_drawdown_jpy": 1760.0, "initial_capital": 30_000_000.0, "max_drawdown_pct_of_capital": 1760.0 / 30_000_000 * 100})

	buy := func(price, bps float64) float64 { return 100 * (price - price/(1+bps/1e4)) }
	sell := func(price, bps float64) float64 { return 100 * (price/(1-bps/1e4) - price) }
	slippage := buy(1000, 5) + sell(1006, 3) + sell(1000, 4) + buy(1010, 6) + buy(1000, 2) + sell(1003, 2) + buy(1000, 1) + sell(1002, 1)
	q4 := q("Q4")
	expect(t, "Q4", q4[0], map[string]any{
		"n": 6.0, "net_pnl_jpy": -1000.0, "fees_jpy": 100.0, "gross_before_fees_jpy": -900.0,
		"slippage_jpy": slippage, "gross_before_costs_jpy": -900 + slippage, "slippage_recorded_fills": 8.0,
	})

	q5 := q("Q5")
	expect(t, "Q5 v1", find(q5, map[string]any{"label_version": 1, "horizon_minutes": 5}), map[string]any{"n": 2.0, "hit_rate": 0.5, "brier": 0.265})
	expect(t, "Q5 v2", find(q5, map[string]any{"label_version": 2, "horizon_minutes": 10}), map[string]any{"n": 4.0, "hit_rate": 0.5, "brier": 0.165})

	q6 := q("Q6")
	expect(t, "Q6 high", find(q6, map[string]any{"confidence_band": ">=0.80"}), map[string]any{"n": 2.0, "ev_net_bps": 10.0})
	expect(t, "Q6 low", find(q6, map[string]any{"confidence_band": "<0.60"}), map[string]any{"n": 1.0, "ev_net_bps": -3.0})

	q7 := q("Q7")
	for _, band := range []string{"<1s", "1-5s", ">=5s", "unknown"} {
		expect(t, "Q7 "+band, find(q7, map[string]any{"data_age": band}), map[string]any{"n": 1.0})
	}

	q8 := q("Q8")
	expect(t, "Q8 v-a", find(q8, map[string]any{"version": "v-a"}), map[string]any{
		"linked_trades": 4.0, "predicted_ev_bps": 25.0, "realized_ev_bps": 0.5, "gap_bps": -24.5, "oos_auc": 0.62, "oos_expected_value_bps": 7.5,
	})
	expect(t, "Q8 v-old", find(q8, map[string]any{"version": "v-old"}), map[string]any{"linked_trades": 0.0, "predicted_ev_bps": nil})

	q9 := q("Q9")
	expect(t, "Q9 v-old", find(q9, map[string]any{"version": "v-old"}), map[string]any{"paper_trades": 2.0, "paper_net_pnl_jpy": -440.0})
	expect(t, "Q9 v-a", find(q9, map[string]any{"version": "v-a"}), map[string]any{"paper_trades": 0.0, "paper_ev_bps": nil})
}

// Opening the connection read-only proves the queries never write.
func TestVerifyQueries_ReadOnlyConnection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ro.db")
	seed, err := sqlitedb.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	actualstest.Seed(t, seed)
	_ = seed.Close()

	names, queries := verifyQueries(t)
	ro, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		t.Fatalf("open ro: %v", err)
	}
	defer func() { _ = ro.Close() }()
	for _, name := range names {
		run(t, ro, name, queries[name])
	}
}
