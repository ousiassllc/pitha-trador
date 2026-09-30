package pages_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/web/molecules"
	"github.com/ousiassllc/pitha-trador/internal/web/pages"
)

// Header's version link lands on `/settings#update-panel` (issue #241): the
// panel must come before the (long) secret-field list, or the anchor sits
// below the first screen.
func TestSettingsPage_UpdatePanelPrecedesSecretFields(t *testing.T) {
	props := pages.SettingsProps{Fields: []molecules.SecretFieldRowProps{{Key: "some_key", Label: "SOME_FIELD_LABEL"}}}
	var buf bytes.Buffer
	if err := pages.SettingsPage(props).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	body := buf.String()

	panel := strings.Index(body, `id="update-panel"`)
	field := strings.Index(body, "SOME_FIELD_LABEL")
	if panel < 0 || field < 0 || panel > field {
		t.Fatalf("#update-panel at %d, first secret field at %d; want the panel first", panel, field)
	}
}
