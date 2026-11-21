package organisms_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/web/handler/migration"
	"github.com/ousiassllc/pitha-trador/internal/web/organisms"
)

// issue #1177: pitha-migration.ts holds no URL; the panel injects every
// endpoint it calls as a data-*-url attribute on its root <section>.
func TestMigrationPanel_InjectsEndpointURLs(t *testing.T) {
	var buf bytes.Buffer
	if err := organisms.MigrationPanel().Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	body := buf.String()
	section := body[:strings.Index(body, ">")+1]
	for _, want := range []string{
		`data-preview-url="` + migration.PreviewPath + `"`,
		`data-confirm-url="/api/v1/migration/confirm"`,
		`data-cancel-url="/api/v1/migration/cancel"`,
		`data-status-url="/api/v1/migration/status"`,
	} {
		if !strings.Contains(section, want) {
			t.Errorf("panel <section> lacks %q: %s", want, section)
		}
	}
	if !strings.Contains(body, `action="/api/v1/migration/export"`) {
		t.Errorf("export form action missing: %s", body)
	}
}
