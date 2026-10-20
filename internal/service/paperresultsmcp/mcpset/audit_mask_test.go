package mcpset

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/mcpwritesettings"
	"github.com/ousiassllc/pitha-trador/internal/service/opsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

// Masking policy: a secret-class (or unknown) key's values never reach the
// audit table, whoever wrote the row. Only recordOn can be given such a key
// (Audited records whitelisted keys only), so this test is in-package.
func TestAuditMasksSecretClassKeys(t *testing.T) {
	ctx := context.Background()
	_, db := testkit.NewMigratedDB(t)
	before, after := opsettings.Value{Current: "old-secret", Overridden: true}, opsettings.Value{Current: "new-secret", Overridden: true}
	if err := recordOn(ctx, db, time.Date(2026, 10, 18, 3, 0, 0, 0, time.UTC), "broker.tachibana.password", before, after, false); err != nil {
		t.Fatal(err)
	}
	var b, a string
	if err := db.QueryRow(`SELECT before_value, after_value FROM mcp_settings_audit`).Scan(&b, &a); err != nil {
		t.Fatal(err)
	}
	if b != mcpwritesettings.Masked || a != mcpwritesettings.Masked {
		t.Fatalf("stored audit values = %q / %q, want both masked", b, a)
	}
}
