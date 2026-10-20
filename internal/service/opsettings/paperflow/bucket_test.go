package paperflow_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/bucketsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/papersettings"
	"github.com/ousiassllc/pitha-trador/internal/service/opsettings"
)

// seededStore is a store after the one-time seed: capital 500,000, stock 500,000,
// bond_etf 0.
func seededStore() *fakeStore {
	return &fakeStore{rows: map[string]string{
		papersettings.KeyInitialCapitalJPY: "500000",
		bucketsettings.KeyStockBudgetJPY:   "500000",
		bucketsettings.KeyBondETFBudgetJPY: "0",
	}}
}

func requireInvalid(t *testing.T, err error) {
	t.Helper()
	var iv *opsettings.InvalidValueError
	if !errors.As(err, &iv) {
		t.Fatalf("err = %v, want *InvalidValueError", err)
	}
}

func TestSave_BucketSumExceedsCapitalRejected(t *testing.T) {
	ctx := context.Background()
	store := seededStore()
	svc := newService(store)

	// 500,000 stock + 1 bond_etf is over the 500,000 capital.
	requireInvalid(t, svc.Save(ctx, bucketsettings.KeyBondETFBudgetJPY, "1"))
	if store.rows[bucketsettings.KeyBondETFBudgetJPY] != "0" {
		t.Errorf("bond_etf row = %q after the rejected save, want the stored 0", store.rows[bucketsettings.KeyBondETFBudgetJPY])
	}
	// A stock budget above the capital on its own.
	requireInvalid(t, svc.Save(ctx, bucketsettings.KeyStockBudgetJPY, "500001"))

	// Making room first lets the same save through; the sum may equal the capital.
	if err := svc.Save(ctx, bucketsettings.KeyStockBudgetJPY, "200000"); err != nil {
		t.Fatal(err)
	}
	if err := svc.Save(ctx, bucketsettings.KeyBondETFBudgetJPY, "300000"); err != nil {
		t.Fatalf("a split summing to exactly the capital was rejected: %v", err)
	}
	requireInvalid(t, svc.Save(ctx, bucketsettings.KeyBondETFBudgetJPY, "300001"))
}

func TestSave_LoweringCapitalBelowBucketSumRejected(t *testing.T) {
	ctx := context.Background()
	store := seededStore()
	svc := newService(store)

	requireInvalid(t, svc.Save(ctx, papersettings.KeyInitialCapitalJPY, "400000"))
	if store.rows[papersettings.KeyInitialCapitalJPY] != "500000" {
		t.Errorf("capital row = %q after the rejected save, want the stored 500000", store.rows[papersettings.KeyInitialCapitalJPY])
	}

	if err := svc.Save(ctx, bucketsettings.KeyStockBudgetJPY, "400000"); err != nil {
		t.Fatal(err)
	}
	if err := svc.Save(ctx, papersettings.KeyInitialCapitalJPY, "400000"); err != nil {
		t.Fatalf("lowering the capital to the bucket sum was rejected: %v", err)
	}
	// Raising the capital never breaks the sum.
	if err := svc.Save(ctx, papersettings.KeyInitialCapitalJPY, "900000"); err != nil {
		t.Fatal(err)
	}
}

func TestReset_BucketSumExceedsCapitalRejected(t *testing.T) {
	ctx := context.Background()
	store := seededStore()
	svc := newService(store)
	if err := svc.Save(ctx, bucketsettings.KeyStockBudgetJPY, "100000"); err != nil {
		t.Fatal(err)
	}
	if err := svc.Save(ctx, bucketsettings.KeyBondETFBudgetJPY, "400000"); err != nil {
		t.Fatal(err)
	}

	// The stock default is the whole capital: 500,000 + 400,000 is over it.
	requireInvalid(t, svc.Reset(ctx, bucketsettings.KeyStockBudgetJPY))
	if store.rows[bucketsettings.KeyStockBudgetJPY] != "100000" {
		t.Errorf("stock row = %q after the rejected reset, want the stored 100000", store.rows[bucketsettings.KeyStockBudgetJPY])
	}

	// Resetting the capital to the yaml/initial default 500,000 after raising
	// it is fine, but resetting it below the budgets is not.
	if err := svc.Save(ctx, papersettings.KeyInitialCapitalJPY, "900000"); err != nil {
		t.Fatal(err)
	}
	if err := svc.Save(ctx, bucketsettings.KeyBondETFBudgetJPY, "800000"); err != nil {
		t.Fatal(err)
	}
	requireInvalid(t, svc.Reset(ctx, papersettings.KeyInitialCapitalJPY)) // default 500,000 < 100,000 + 800,000
	if store.rows[papersettings.KeyInitialCapitalJPY] != "900000" {
		t.Errorf("capital row = %q after the rejected reset, want the stored 900000", store.rows[papersettings.KeyInitialCapitalJPY])
	}
}

func TestSave_BucketKeysBeforeSeedUseTheDefaultSplit(t *testing.T) {
	ctx := context.Background()
	store := &fakeStore{rows: map[string]string{}}
	svc := newService(store)

	// No rows: stock defaults to the whole capital (500,000), bond_etf to 0.
	if v, err := svc.Get(ctx, bucketsettings.KeyStockBudgetJPY); err != nil || v.Current != "500000" || v.Overridden {
		t.Fatalf("stock before the seed = %+v, %v; want the whole capital, not overridden", v, err)
	}
	if v, err := svc.Get(ctx, bucketsettings.KeyBondETFBudgetJPY); err != nil || v.Current != "0" || v.Overridden {
		t.Fatalf("bond_etf before the seed = %+v, %v; want 0, not overridden", v, err)
	}
	requireInvalid(t, svc.Save(ctx, bucketsettings.KeyBondETFBudgetJPY, "1"))
	if err := svc.Save(ctx, bucketsettings.KeyStockBudgetJPY, "0"); err != nil {
		t.Fatal(err)
	}
	if err := svc.Save(ctx, bucketsettings.KeyBondETFBudgetJPY, "500000"); err != nil {
		t.Fatal(err)
	}
	// Lowering the capital while only the stock row is absent follows it.
	requireInvalid(t, svc.Save(ctx, papersettings.KeyInitialCapitalJPY, "400000"))
}

func TestSave_BucketBudgetMustBeAWholeNonNegativeNumber(t *testing.T) {
	ctx := context.Background()
	svc := newService(seededStore())
	for _, raw := range []string{"-1", "1.5", "abc", "1000000000001"} {
		if err := svc.Save(ctx, bucketsettings.KeyStockBudgetJPY, raw); err == nil {
			t.Errorf("Save(%q) succeeded, want a rejection", raw)
		}
	}
	if err := svc.Save(ctx, bucketsettings.KeyStockBudgetJPY, "0"); err != nil {
		t.Errorf("Save(0) = %v, want accepted (the bucket is not used)", err)
	}
}
