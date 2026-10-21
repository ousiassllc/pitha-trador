package bucketsettings_test

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/bucketsettings"
)

// Issue #835: with a fractional operating capital and no stored stock row, the
// budget the Risk Engine judges with (Effective -> Resolve), the one the
// Settings screen shows and Reset stores (DefaultText) and the one the seed
// writes are the same whole-yen value.
func TestStockDefault_RiskScreenResetAndSeedAgreeForAFractionalCapital(t *testing.T) {
	ctx := context.Background()
	const capital = 500000.5
	const want = 500000.0

	if got := bucketsettings.DefaultStockBudgetJPY(capital); got != want {
		t.Fatalf("DefaultStockBudgetJPY = %v, want %v", got, want)
	}

	risk, err := bucketsettings.Effective(ctx, &seedStore{rows: map[string]string{}}, capital)
	if err != nil {
		t.Fatal(err)
	}
	if risk.StockJPY != want {
		t.Errorf("Risk's stock budget = %v, want %v", risk.StockJPY, want)
	}
	if got := (bucketsettings.Overrides{}).Resolve(capital).StockJPY; got != want {
		t.Errorf("Resolve stock = %v, want %v", got, want)
	}

	text := bucketsettings.DefaultText(bucketsettings.KeyStockBudgetJPY, capital)
	if shown, err := strconv.ParseFloat(text, 64); err != nil || shown != risk.StockJPY {
		t.Errorf("DefaultText = %q (%v), want the Risk value %v", text, err, risk.StockJPY)
	}

	seeded := &seedStore{rows: map[string]string{}}
	if err := bucketsettings.SeedDefaults(ctx, seeded, capital, seedNow); err != nil {
		t.Fatal(err)
	}
	if seeded.rows[bucketsettings.KeyStockBudgetJPY] != text {
		t.Errorf("seeded stock row = %q, want %q", seeded.rows[bucketsettings.KeyStockBudgetJPY], text)
	}

	// The stored default is what the budgets then resolve to, and it fits the capital.
	stored, err := bucketsettings.Effective(ctx, seeded, capital)
	if err != nil {
		t.Fatal(err)
	}
	if stored != risk {
		t.Errorf("budgets after seeding = %+v, want the unseeded default %+v", stored, risk)
	}
	if err := bucketsettings.CheckSave(ctx, &seedStore{rows: map[string]string{}}, capital, bucketsettings.KeyStockBudgetJPY, text); err != nil {
		t.Errorf("CheckSave(default stock text) = %v, want accepted", err)
	}
}

// An integer capital (the default configuration) is unchanged by the rounding.
func TestStockDefault_IntegerCapitalIsUnchanged(t *testing.T) {
	if got := (bucketsettings.Overrides{}).Resolve(500000).StockJPY; got != 500000 {
		t.Errorf("Resolve stock = %v, want 500000", got)
	}
}

// Issue #844: the user-facing reason of a refusal carries no package prefix;
// the error itself keeps it as log context.
func TestRejections_ReasonHasNoPackagePrefix(t *testing.T) {
	over := bucketsettings.CheckTotal(bucketsettings.Budgets{StockJPY: 300000, BondETFJPY: 300000}, 500000)
	rangeErr := bucketsettings.Validate(bucketsettings.KeyStockBudgetJPY, -1)
	for name, err := range map[string]error{"CheckTotal": over, "Validate": rangeErr} {
		var rejected *bucketsettings.RejectedError
		if !errors.As(err, &rejected) {
			t.Fatalf("%s err = %v, want *RejectedError", name, err)
		}
		if strings.Contains(rejected.Reason, "bucketsettings:") || rejected.Reason == "" {
			t.Errorf("%s reason = %q, want a non-empty reason without the package prefix", name, rejected.Reason)
		}
		if !strings.HasPrefix(err.Error(), "bucketsettings: ") {
			t.Errorf("%s Error() = %q, want the package context kept for logs", name, err.Error())
		}
	}
}
