package mcproute_test

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/router"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/mcpset"
)

// fakeGate is the write switch and audit log behind the Settings toggle.
type fakeGate struct {
	enabled bool
	rows    []mcpset.Entry
	setErr  error
}

func (g *fakeGate) Enabled(context.Context) (bool, error) { return g.enabled, nil }

func (g *fakeGate) SetEnabled(_ context.Context, on bool) error {
	if g.setErr != nil {
		return g.setErr
	}
	g.enabled = on
	return nil
}

func (g *fakeGate) Recent(context.Context, int) ([]mcpset.Entry, error) { return g.rows, nil }

func newAppWithGate(t *testing.T, g *fakeGate) *app {
	t.Helper()
	a := newApp(t, fullSecrets{})
	a.engine = router.New(router.WithSecretsStore(fullSecrets{}), router.WithMCPSettings(a.svc), router.WithMCPWriteGate(g))
	a.session()
	return a
}

// Without the gate the panel has no write section and the route is absent.
func TestWriteSectionNeedsTheGate(t *testing.T) {
	a := newApp(t, fullSecrets{})
	a.session()
	if body := a.get("/settings/mcp-panel").Body.String(); strings.Contains(body, "mcp-write") {
		t.Error("the write section is shown without a gate")
	}
	rec := a.post("/settings/mcp/write", url.Values{"enabled": {"on"}}, true)
	if rec.Code != http.StatusNotFound {
		t.Errorf("POST /settings/mcp/write without a gate = %d, want 404", rec.Code)
	}
}

// The Settings toggle shows the switch (on by default), flips it with a POST
// (session + CSRF like every state change) and lists the audit rows.
func TestWriteToggleOnTheSettingsPanel(t *testing.T) {
	before, after := "1", "0"
	g := &fakeGate{enabled: true, rows: []mcpset.Entry{
		{ID: 2, At: "2026-10-20T01:00:00Z", Actor: mcpset.ActorMCP, Client: "100.64.0.9", Action: mcpset.ActionSet, Key: "risk.paper.max_trade_loss_pct", After: &after},
		{ID: 1, At: "2026-10-19T01:00:00Z", Actor: mcpset.ActorGUI, Action: mcpset.ActionWriteDisable, Key: "mcp.settings_write_enabled", Before: &before, After: &after},
	}}
	a := newAppWithGate(t, g)

	body := a.get("/settings/mcp-panel").Body.String()
	for _, want := range []string{`data-testid="mcp-write-enabled"`, "checked", "MCP からの設定変更を許可する", "mcp-audit-row", "MCP（100.64.0.9）", "Settings 画面", "risk.paper.max_trade_loss_pct", "書き込みを禁止", "（既定）"} {
		if !strings.Contains(body, want) {
			t.Errorf("panel lacks %q", want)
		}
	}

	// A POST without the CSRF token changes nothing.
	if rec := a.post("/settings/mcp/write", url.Values{}, false); rec.Code == http.StatusOK || !g.enabled {
		t.Fatalf("POST without CSRF = %d, enabled=%v", rec.Code, g.enabled)
	}
	// Unchecked box = off.
	rec := a.post("/settings/mcp/write", url.Values{}, true)
	if rec.Code != http.StatusOK || g.enabled {
		t.Fatalf("POST off = %d, enabled=%v", rec.Code, g.enabled)
	}
	if !strings.Contains(rec.Body.String(), "禁止しました") || strings.Contains(rec.Body.String(), `data-testid="mcp-write-enabled" checked`) {
		t.Errorf("panel after switching off: %s", rec.Body.String())
	}
	rec = a.post("/settings/mcp/write", url.Values{"enabled": {"on"}}, true)
	if rec.Code != http.StatusOK || !g.enabled || !strings.Contains(rec.Body.String(), "許可しました") {
		t.Fatalf("POST on = %d, enabled=%v", rec.Code, g.enabled)
	}
	// The cache is off for these responses like the rest of the MCP panel.
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Error("write response is cacheable")
	}
}

// A failed toggle (for example its audit row could not be written, so the
// switch change was rolled back, issue #818) answers 500 with the "not saved"
// toast that the page routes into #toast-region.
func TestWriteToggleFailureIsReported(t *testing.T) {
	g := &fakeGate{enabled: true, setErr: errors.New("disk full")}
	a := newAppWithGate(t, g)
	rec := a.post("/settings/mcp/write", url.Values{}, true)
	if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "disk full") {
		t.Fatalf("failure = %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `data-testid="toast"`) || !strings.Contains(rec.Body.String(), "設定変更の許可を保存できませんでした") {
		t.Errorf("failure has no 'not saved' toast: %s", rec.Body.String())
	}
	if !g.enabled {
		t.Error("a failed save changed the switch")
	}
}
