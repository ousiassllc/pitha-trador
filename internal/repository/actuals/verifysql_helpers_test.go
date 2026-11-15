package actuals_test

import (
	"database/sql"
	"fmt"
	"math"
	"testing"
)

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
