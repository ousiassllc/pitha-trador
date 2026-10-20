package bucketsettings_test

import (
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/bucketsettings"
)

func TestApply_AcceptsWholeYenFromZero(t *testing.T) {
	var o bucketsettings.Overrides
	for _, tc := range []struct {
		key, raw string
		ok       bool
	}{
		{bucketsettings.KeyStockBudgetJPY, "0", true},
		{bucketsettings.KeyStockBudgetJPY, "500000", true},
		{bucketsettings.KeyBondETFBudgetJPY, "1000000000000", true},
		{bucketsettings.KeyBondETFBudgetJPY, "-1", false},
		{bucketsettings.KeyBondETFBudgetJPY, "0.5", false},
		{bucketsettings.KeyStockBudgetJPY, "1000000000001", false},
		{bucketsettings.KeyStockBudgetJPY, `"x"`, false},
		{"risk.bucket.crypto.budget_jpy", "1", false},
	} {
		if err := bucketsettings.Apply(&o, tc.key, tc.raw); (err == nil) != tc.ok {
			t.Errorf("Apply(%s, %s) = %v, want ok=%v", tc.key, tc.raw, err, tc.ok)
		}
	}
}

func TestResolve_DefaultsAreTheWholeCapitalForStockAndZeroForBondETF(t *testing.T) {
	got := bucketsettings.Overrides{}.Resolve(500000)
	if got.StockJPY != 500000 || got.BondETFJPY != 0 {
		t.Errorf("Resolve = %+v, want stock 500000 / bond_etf 0", got)
	}
	n := 100.0
	got = bucketsettings.Overrides{StockBudgetJPY: &n}.Resolve(500000)
	if got.StockJPY != 100 || got.BondETFJPY != 0 {
		t.Errorf("Resolve with a stock row = %+v", got)
	}
}

func TestCheckTotal_SumMayEqualButNotExceedTheCapital(t *testing.T) {
	if err := bucketsettings.CheckTotal(bucketsettings.Budgets{StockJPY: 200000, BondETFJPY: 300000}, 500000); err != nil {
		t.Errorf("sum == capital rejected: %v", err)
	}
	if err := bucketsettings.CheckTotal(bucketsettings.Budgets{StockJPY: 200000, BondETFJPY: 300001}, 500000); err == nil {
		t.Error("sum > capital accepted")
	}
}
