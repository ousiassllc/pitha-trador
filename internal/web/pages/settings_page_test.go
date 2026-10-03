package pages_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/web/molecules"
	"github.com/ousiassllc/pitha-trador/internal/web/pages"
)

func renderSettings(t *testing.T, props pages.SettingsProps) string {
	t.Helper()
	var buf bytes.Buffer
	if err := pages.SettingsPage(props).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	return buf.String()
}

func testConnections() []molecules.ConnectionProps {
	return []molecules.ConnectionProps{
		{ID: "alpha", Name: "Alpha", Required: true, Fields: []molecules.SecretFieldRowProps{
			{Key: "alpha_key", Label: "ALPHA_KEY", Configured: true},
			{Key: "alpha_url", Label: "ALPHA_URL"},
		}},
		{ID: "beta", Name: "Beta", Fields: []molecules.SecretFieldRowProps{{Key: "beta_key", Label: "BETA_KEY"}}},
	}
}

// issue #302: each connection is a card whose button opens a dialog holding
// all of that connection's fields - and only those.
func TestSettingsPage_EachConnectionOpensItsOwnModalWithAllItsFields(t *testing.T) {
	body := renderSettings(t, pages.SettingsProps{Connections: testConnections()})

	for _, id := range []string{"alpha", "beta"} {
		for _, want := range []string{`data-testid="settings-card-` + id + `"`, `data-modal-open="modal-` + id + `"`, `<dialog id="modal-` + id + `"`} {
			if !strings.Contains(body, want) {
				t.Errorf("SettingsPage lacks %q", want)
			}
		}
	}
	alpha := strings.Index(body, `<dialog id="modal-alpha"`)
	beta := strings.Index(body, `<dialog id="modal-beta"`)
	for _, label := range []string{"ALPHA_KEY", "ALPHA_URL"} {
		if i := strings.Index(body[alpha:], label); i < 0 || alpha+i > beta {
			t.Errorf("%s is not inside modal-alpha", label)
		}
	}
	if i := strings.Index(body, "BETA_KEY"); i < beta {
		t.Errorf("BETA_KEY at %d is before modal-beta at %d", i, beta)
	}
}

// The list shows the 設定済み/未設定 state per connection, and 必須 while a
// required connection is unset.
func TestSettingsPage_CardShowsConnectionState(t *testing.T) {
	body := renderSettings(t, pages.SettingsProps{Connections: testConnections()})

	for id, state := range map[string]string{"alpha": "configured", "beta": "unset"} {
		want := `id="connection-status-` + id + `" class="inline-flex flex-wrap items-center gap-1" data-state="` + state + `"`
		if !strings.Contains(body, want) {
			t.Errorf("connection %s: want %q in body", id, want)
		}
	}

	unset := testConnections()
	unset[0].Fields[0].Configured = false
	body = renderSettings(t, pages.SettingsProps{Connections: unset})
	if !strings.Contains(body, ">必須<") {
		t.Error("an unset required connection lacks the 必須 badge")
	}
}

// Header's version link lands on `/settings#update-panel` (issue #241): the
// panel now lives in a modal, which pitha-modal opens from the hash, so the
// panel must sit inside a <dialog> and the modal script must be loaded.
func TestSettingsPage_UpdatePanelLivesInModalOpenedByHash(t *testing.T) {
	body := renderSettings(t, pages.SettingsProps{})

	dialog := strings.Index(body, `<dialog id="modal-update"`)
	panel := strings.Index(body, `id="update-panel"`)
	if dialog < 0 || panel < dialog || panel > dialog+strings.Index(body[dialog:], "</dialog>") {
		t.Fatalf("#update-panel at %d is not inside <dialog id=modal-update> at %d", panel, dialog)
	}
	if !strings.Contains(body, `/static/dist/js/modal/pitha-modal.js`) {
		t.Error("SettingsPage does not load pitha-modal.js")
	}
}

// Modals are native dialogs labelled by their title (a11y, issue #302).
func TestSettingsPage_ModalsAreLabelledDialogs(t *testing.T) {
	body := renderSettings(t, pages.SettingsProps{Connections: testConnections()})
	for _, id := range []string{"modal-alpha", "modal-update", "modal-error-log"} {
		for _, want := range []string{`<dialog id="` + id + `"`, `aria-labelledby="` + id + `-title"`, `id="` + id + `-title"`, `data-testid="` + id + `-close"`} {
			if !strings.Contains(body, want) {
				t.Errorf("modal %s lacks %q", id, want)
			}
		}
	}
}

// A modal <dialog> makes everything outside it inert, `#toast-region`
// included, so each dialog must carry its own toast region for failure toasts
// to stay dismissible (issue #353).
func TestSettingsPage_EachModalHasItsOwnToastRegion(t *testing.T) {
	body := renderSettings(t, pages.SettingsProps{Connections: testConnections()})
	for _, id := range []string{"modal-alpha", "modal-update", "modal-error-log"} {
		start := strings.Index(body, `<dialog id="`+id+`"`)
		if start < 0 {
			t.Fatalf("no <dialog id=%s>", id)
		}
		dialog := body[start : start+strings.Index(body[start:], "</dialog>")]
		if got := strings.Count(dialog, "data-toast-region"); got != 1 {
			t.Errorf("modal %s has %d data-toast-region elements, want 1", id, got)
		}
	}
}

// The エラーログ section is a plain GET form to the download API (FR-ERRLOG-1):
// 7 days and ERROR-only preselected, HTMX kept out via hx-disable.
func TestSettingsPage_ErrorLogPanelIsPlainDownloadForm(t *testing.T) {
	body := renderSettings(t, pages.SettingsProps{})

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

// FR-ERRLOG-1: the エラーログ section lives on Settings only; the first-run
// Setup screen (no Header, no session-guarded fragments) must not offer it.
func TestSetupPage_HasNoErrorLogPanel(t *testing.T) {
	for _, complete := range []bool{false, true} {
		var buf bytes.Buffer
		if err := pages.SetupPage(pages.SetupProps{Complete: complete}).Render(context.Background(), &buf); err != nil {
			t.Fatalf("render: %v", err)
		}
		body := buf.String()
		for _, unwanted := range []string{`error-log-panel`, `/api/v1/logs/errors`} {
			if strings.Contains(body, unwanted) {
				t.Errorf("complete=%v: SetupPage contains %q, want the error log panel on Settings only", complete, unwanted)
			}
		}
	}
}

// issue #302: Setup uses the same connection list (cards + modals) as
// Settings, and still shows the completion state.
func TestSetupPage_UsesConnectionListAndModals(t *testing.T) {
	var buf bytes.Buffer
	props := pages.SetupProps{Connections: testConnections()}
	if err := pages.SetupPage(props).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	body := buf.String()
	for _, want := range []string{`data-testid="connection-list"`, `data-testid="settings-card-alpha"`, `<dialog id="modal-alpha"`, `ALPHA_URL`, `setup-incomplete`, `/static/dist/js/modal/pitha-modal.js`} {
		if !strings.Contains(body, want) {
			t.Errorf("SetupPage lacks %q", want)
		}
	}
}

// issue #352: "続ける" is only offered once every required key is stored;
// before that the Setup Guard would just bounce it back to /setup.
func TestSetupPage_ContinueLinkOnlyWhenComplete(t *testing.T) {
	render := func(complete bool) string {
		var buf bytes.Buffer
		if err := pages.SetupPage(pages.SetupProps{Complete: complete}).Render(context.Background(), &buf); err != nil {
			t.Fatalf("render: %v", err)
		}
		return buf.String()
	}

	if body := render(false); strings.Contains(body, `data-testid="setup-continue"`) || strings.Contains(body, `href="/scanner"`) {
		t.Errorf("incomplete SetupPage must not render the 続ける link; body=%s", body)
	}
	body := render(true)
	if !strings.Contains(body, `data-testid="setup-continue"`) || !strings.Contains(body, `href="/scanner"`) {
		t.Errorf("complete SetupPage lacks the 続ける link to /scanner; body=%s", body)
	}
	if status := strings.Index(body, `id="setup-status"`); status < 0 || strings.Index(body, `data-testid="setup-continue"`) < status {
		t.Errorf("the 続ける link must sit inside #setup-status so the OOB swap replaces it; body=%s", body)
	}
}
