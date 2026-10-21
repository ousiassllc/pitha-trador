package bucket_test

import (
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/bucketsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/risk/bucket"
)

func TestOf_AssetClassPicksItsOwnBucket(t *testing.T) {
	for assetClass, want := range map[string]string{
		"":                       domain.BucketStock,
		domain.AssetClassStock:   domain.BucketStock,
		domain.AssetClassBondETF: domain.BucketBondETF,
	} {
		if got, err := bucket.Of(assetClass); err != nil || got != want {
			t.Errorf("Of(%q) = (%q, %v), want %q", assetClass, got, err, want)
		}
	}
	if _, err := bucket.Of("crypto"); err == nil {
		t.Error("Of(crypto) = nil error, want an error")
	}
}

func TestBudgetOf_PicksTheBucketsBudget(t *testing.T) {
	b := bucketsettings.Budgets{StockJPY: 300, BondETFJPY: 100}
	if got := bucket.BudgetOf(b, domain.BucketStock); got != 300 {
		t.Errorf("BudgetOf(stock) = %v, want 300", got)
	}
	if got := bucket.BudgetOf(b, domain.BucketBondETF); got != 100 {
		t.Errorf("BudgetOf(bond_etf) = %v, want 100", got)
	}
}
