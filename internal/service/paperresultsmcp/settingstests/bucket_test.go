package settingstests

import (
	"net/http"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
)

const (
	stockKey   = "risk.bucket.stock.budget_jpy"
	bondKey    = "risk.bucket.bond_etf.budget_jpy"
	capitalKey = "risk.paper.initial_capital_jpy"
)

// settings_get lists both bucket budgets with their defaults (the stock bucket
// the whole capital, bond_etf 0), and settings_diff shows a saved budget
// against that default (issue #824).
func TestBucketBudgetsListedAndDiffed(t *testing.T) {
	srv, _ := settingsServer(t, nil)
	st := getState(srv, t)
	for key, want := range map[string]string{stockKey: "500000", bondKey: "0"} {
		it := st.item(t, key)
		if it.Value != want || it.Default != want || it.Source != "default" || it.Unit != "yen" {
			t.Errorf("%s on a fresh database = %+v, want default %s yen", key, it, want)
		}
	}

	set(srv, t, stockKey, 300000).data(t, "settings_set", &struct{}{})
	var d settingsDiff
	srv.call(t, "settings_diff", map[string]any{}).data(t, "settings_diff", &d)
	if len(d.Changes) != 1 || d.Changes[0].Key != stockKey ||
		d.Changes[0].Before.Value != "500000" || d.Changes[0].Before.Source != "default" ||
		d.Changes[0].After.Value != "300000" || d.Changes[0].After.Source != "stored" {
		t.Fatalf("diff = %+v", d)
	}
}

// A save that would take the budgets' sum over the operating capital is
// refused through the MCP tool before anything is stored, so it leaves neither
// a changed value nor an audit row; the saves that keep the sum within the
// capital are stored with their audit row. Moving budget between the buckets
// works by saving the shrinking one first (issue #824).
func TestBucketSumOverCapitalRejectedLeavesNoAuditRow(t *testing.T) {
	srv, w := settingsServer(t, nil)
	audits := func() int { return count(t, w, `SELECT COUNT(*) FROM mcp_settings_audit`) }
	refused := func(key string, value any) {
		t.Helper()
		before := audits()
		set(srv, t, key, value).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidValue)
		if n := audits(); n != before {
			t.Fatalf("audit rows = %d after a refused save of %s, want %d", n, key, before)
		}
	}

	// Stock defaults to the whole capital (500000), so any bond_etf budget is too much.
	refused(bondKey, 100000)
	if _, ok := storedValue(t, w, bondKey); ok {
		t.Fatal("the refused bond_etf budget was stored")
	}

	// Shrink stock first, then grow bond_etf: 300000 + 200000 = capital.
	set(srv, t, stockKey, 300000).data(t, "settings_set", &struct{}{})
	set(srv, t, bondKey, 200000).data(t, "settings_set", &struct{}{})
	if n := audits(); n != 2 {
		t.Fatalf("audit rows = %d after two accepted saves, want 2", n)
	}

	refused(stockKey, 400000)   // 400000 + 200000 > 500000
	refused(capitalKey, 400000) // capital below the 500000 sum
	if v, _ := storedValue(t, w, stockKey); v != "300000" {
		t.Fatalf("stock budget = %q after the refused saves, want 300000", v)
	}
	if _, ok := storedValue(t, w, capitalKey); ok {
		t.Fatal("the refused capital was stored")
	}
}
