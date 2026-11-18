package mcproute_test

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/mcpset"
)

// indexOfAll returns the index of each marker in body, in the order given,
// failing the test when one is missing.
func indexOfAll(t *testing.T, body string, markers []string) []int {
	t.Helper()
	at := make([]int, len(markers))
	for i, m := range markers {
		at[i] = strings.Index(body, m)
		if at[i] < 0 {
			t.Fatalf("body lacks %q", m)
		}
	}
	return at
}

// The MCP dialog leads with the connection: state and 起動/停止, then the
// enable switch and listen address, the API key, the connection URL, and only
// then the permission to change settings via MCP, the order-proposal intake
// and the audit log. The key and URL must not be pushed down by anything else.
func TestMCPPanel_OrdersConnectionBeforeWritePermissionAndAuditLog(t *testing.T) {
	before := "1"
	g := &fakeGate{enabled: true, rows: []mcpset.Entry{{ID: 1, At: "2026-10-01T00:00:00Z", Actor: mcpset.ActorMCP, Action: mcpset.ActionSet, Key: "risk.paper.max_trade_loss_pct", After: &before}}}
	a := newAppWithGate(t, g)
	body := a.get("/settings/mcp-panel").Body.String()

	markers := []string{
		`data-testid="mcp-state"`,
		`data-testid="mcp-process-controls"`,
		`data-testid="mcp-enabled"`,
		`data-testid="mcp-listen-input"`,
		`data-testid="mcp-key"`,
		`data-testid="mcp-url"`,
		`data-testid="mcp-write-form"`,
		`data-testid="mcp-order-proposals-form"`,
		`data-testid="mcp-audit"`,
	}
	at := indexOfAll(t, body, markers)
	for i := 1; i < len(at); i++ {
		if at[i-1] >= at[i] {
			t.Errorf("%s (at %d) must come before %s (at %d)", markers[i-1], at[i-1], markers[i], at[i])
		}
	}
	// The audit log tells whether a change came from the MCP or the screen.
	if !strings.Contains(body, "変更元") || !strings.Contains(body, "MCP") {
		t.Errorf("the audit log does not show where a change came from")
	}
	// The experiments are not part of the MCP dialog's content any more.
	for _, gone := range []string{"mcp-experiments", "mcp-experiment-form", "/settings/mcp/experiments"} {
		if strings.Contains(body, gone) {
			t.Errorf("the MCP panel still holds %q", gone)
		}
	}
}

// dialogBody returns the markup of the <dialog> with the given id.
func dialogBody(t *testing.T, page, id string) string {
	t.Helper()
	m := regexp.MustCompile(`(?s)<dialog[^>]*\bid="` + regexp.QuoteMeta(id) + `".*?</dialog>`).FindString(page)
	if m == "" {
		t.Fatalf("page has no dialog %q", id)
	}
	return m
}

// 調整の実験 has its own card and dialog on the Settings page, outside the MCP
// dialog; the dialog loads the list first, and the manual record form is
// closed (<details>, opened by 「手で記録する」).
func TestExperiments_LiveOutsideTheMCPDialogAndManualFormStartsClosed(t *testing.T) {
	a, _ := newExperimentsApp(t)
	page := a.get("/settings/integrations").Body.String()

	if !strings.Contains(page, `data-testid="settings-card-mcp-experiments"`) || !strings.Contains(page, `data-modal-open="modal-mcp-experiments"`) {
		t.Fatalf("the Settings page has no card for the experiments dialog")
	}
	mcpDialog := dialogBody(t, page, "modal-mcp")
	for _, gone := range []string{`id="mcp-experiments"`, "mcp-experiments-slot", "/settings/mcp/experiments"} {
		if strings.Contains(mcpDialog, gone) {
			t.Errorf("the MCP dialog still contains %q", gone)
		}
	}
	expDialog := dialogBody(t, page, "modal-mcp-experiments")
	if !strings.Contains(expDialog, `id="mcp-experiments"`) || !strings.Contains(expDialog, `hx-get="/settings/mcp/experiments"`) {
		t.Errorf("the experiments dialog does not load the experiments section: %s", expDialog)
	}

	// Create one so the list is not empty, then check the section as served
	// both initially and after the create response.
	a.post("/settings/mcp/experiments", experimentForm(nil), true)
	for name, body := range map[string]string{
		"initial section": a.get("/settings/mcp/experiments").Body.String(),
		"after a create":  a.post("/settings/mcp/experiments", experimentForm(url.Values{"name": {"risk.max_daily_trades"}}), true).Body.String(),
	} {
		details := regexp.MustCompile(`<details[^>]*data-testid="mcp-experiment-manual"[^>]*>`).FindString(body)
		if details == "" {
			t.Fatalf("%s: no <details data-testid=mcp-experiment-manual>", name)
		}
		if regexp.MustCompile(`\sopen[\s=>]`).MatchString(details) {
			t.Errorf("%s: the manual form is open by default: %s", name, details)
		}
		order := indexOfAll(t, body, []string{
			`data-testid="mcp-experiments-table"`, `data-testid="mcp-experiment-detail"`, `<details`,
			`data-testid="mcp-experiment-form"`, `</details>`,
		})
		for i := 1; i < len(order); i++ {
			if order[i-1] >= order[i] {
				t.Errorf("%s: the list and verdicts must come first and the form sit inside the closed details (marker %d at %d, then %d)", name, i, order[i-1], order[i])
			}
		}
		if !strings.Contains(body, ">手で記録する</summary>") {
			t.Errorf("%s: the details has no 手で記録する summary", name)
		}
	}
}

// Without the write gate there are no experiments, so the page shows no card
// and no dialog for them (and the routes are absent).
func TestExperimentsCardNeedsTheWriteGate(t *testing.T) {
	a := newApp(t, fullSecrets{})
	page := a.get("/settings/integrations").Body.String()
	if strings.Contains(page, `id="modal-mcp-experiments"`) || strings.Contains(page, `data-testid="settings-card-mcp-experiments"`) {
		t.Error("the experiments card is shown without the write gate")
	}
	if rec := a.get("/settings/mcp/experiments"); rec.Code != http.StatusNotFound {
		t.Errorf("GET /settings/mcp/experiments without the gate = %d, want 404", rec.Code)
	}
}
