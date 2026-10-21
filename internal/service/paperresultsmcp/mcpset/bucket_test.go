package mcpset_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/bucketsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/papersettings"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/service/opsettings"
)

// seedBuckets stores the state after the one-time seed: capital 500,000,
// stock 500,000, bond_etf 0.
func (f *fixture) seedBuckets(t *testing.T) {
	t.Helper()
	store := system.NewRuntimeSettingsRepository(f.db)
	for key, value := range map[string]string{
		papersettings.KeyInitialCapitalJPY: "500000",
		bucketsettings.KeyStockBudgetJPY:   "500000",
		bucketsettings.KeyBondETFBudgetJPY: "0",
	} {
		if err := store.Set(context.Background(), key, value, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
}

// Lowering the capital below the bucket budgets' sum is refused on the audited
// paths (the Settings screen and the MCP) before anything is stored, so it
// leaves neither a changed value nor an audit row (issue #824).
func TestLoweringCapitalBelowBucketSumRejectedLeavesNoAuditRow(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	f.seedBuckets(t)
	key := papersettings.KeyInitialCapitalJPY

	err := f.ops.Save(ctx, key, "400000")
	if _, ok := invalidValue(err); !ok {
		t.Fatalf("Save err = %v, want *InvalidValueError", err)
	}
	_, err = f.writer.Set(ctx, "c", key, "400000")
	if _, ok := invalidValue(err); !ok {
		t.Fatalf("Set err = %v, want *InvalidValueError", err)
	}
	if v, err := f.ops.Get(ctx, key); err != nil || v.Current != "500000" {
		t.Fatalf("capital after the rejected saves = %+v, %v; want 500000", v, err)
	}
	if n := f.count(t, "mcp_settings_audit"); n != 0 {
		t.Fatalf("audit rows = %d after rejected saves, want 0", n)
	}

	// A save the budgets allow is stored with its audit row in the same transaction.
	if err := f.ops.Save(ctx, key, "600000"); err != nil {
		t.Fatal(err)
	}
	if n := f.count(t, "mcp_settings_audit"); n != 1 {
		t.Fatalf("audit rows = %d after an accepted save, want 1", n)
	}
}

func invalidValue(err error) (*opsettings.InvalidValueError, bool) {
	var iv *opsettings.InvalidValueError
	return iv, errors.As(err, &iv)
}
