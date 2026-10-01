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

// The エラーログ section is a plain GET form to the download API (FR-ERRLOG-1):
// 7 days and ERROR-only preselected, HTMX kept out via hx-disable.
func TestSettingsPage_ErrorLogPanelIsPlainDownloadForm(t *testing.T) {
	var buf bytes.Buffer
	if err := pages.SettingsPage(pages.SettingsProps{}).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	body := buf.String()

	for _, want := range []string{
		`id="error-log-panel"`,
		`<form method="get" action="/api/v1/logs/errors" hx-disable`,
		`<option value="7" selected>`,
		`<option value="error" selected>`,
		`<option value="warn">`,
		`<option value="90">`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("SettingsPage lacks %q", want)
		}
	}
}

// issue #272: AdvancedFields render inside a <details> that is collapsed
// unless a stored override is present.
func TestSettingsPage_AdvancedFieldsLiveInDetailsOpenedByStoredValue(t *testing.T) {
	render := func(configured bool) string {
		props := pages.SettingsProps{
			Fields:         []molecules.SecretFieldRowProps{{Key: "k1", Label: "FRONT_LABEL"}},
			AdvancedFields: []molecules.SecretFieldRowProps{{Key: "k2", Label: "ADVANCED_LABEL", Configured: configured}},
		}
		var buf bytes.Buffer
		if err := pages.SettingsPage(props).Render(context.Background(), &buf); err != nil {
			t.Fatalf("render: %v", err)
		}
		return buf.String()
	}

	for _, configured := range []bool{false, true} {
		body := render(configured)
		start, end := strings.Index(body, "<details"), strings.Index(body, "</details>")
		front, adv := strings.Index(body, "FRONT_LABEL"), strings.Index(body, "ADVANCED_LABEL")
		if start < 0 || front < 0 || adv < 0 || front >= start || start >= adv || adv >= end {
			t.Fatalf("configured=%v: front=%d details=%d advanced=%d end=%d; want FRONT_LABEL before <details> holding ADVANCED_LABEL", configured, front, start, adv, end)
		}
		if open := strings.Contains(body[start:strings.Index(body[start:], ">")+start], " open"); open != configured {
			t.Errorf("configured=%v: <details> open = %v", configured, open)
		}
	}
}
