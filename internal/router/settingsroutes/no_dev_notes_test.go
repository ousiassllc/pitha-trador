package settingsroutes_test

import (
	"html"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/web/settingspages"
)

// devNotePattern matches the development references that must not reach an
// operator's screen (issue #902): "issue #123" / "PR #123" / "#123で対応",
// a bare "#123", and requirement IDs such as "FR-POLICY-1". Data IDs shown
// as "#<n>" need a row to exist, which an empty test database has none of.
var devNotePattern = regexp.MustCompile(`(?i)\b(?:issue|pr)\s*#?\d+|\bFR-[A-Z]+-\d+|#\d+`)

var (
	scriptStylePattern = regexp.MustCompile(`(?is)<(script|style)\b.*?</(script|style)>`)
	tagPattern         = regexp.MustCompile(`(?s)<[^>]*>`)
	// textAttrPattern picks the attributes that show text to the operator
	// (hover text, accessible name, placeholder, data passed to Lit
	// components as JSON is not shown as-is and is left out).
	textAttrPattern = regexp.MustCompile(`\b(?:title|aria-label|placeholder|hx-confirm|data-confirm)="([^"]*)"`)
)

// visibleText is the text an operator can read or hear on a rendered page:
// the text nodes (help text included, it is in the DOM) plus the
// text-carrying attributes.
func visibleText(body string) string {
	var attrs []string
	for _, m := range textAttrPattern.FindAllStringSubmatch(body, -1) {
		attrs = append(attrs, m[1])
	}
	text := tagPattern.ReplaceAllString(scriptStylePattern.ReplaceAllString(body, " "), " ")
	return html.UnescapeString(text + "\n" + strings.Join(attrs, "\n"))
}

// No screen shows an Issue / PR / FR number (issue #902 受け入れ条件). Every
// screen and fragment the app serves is rendered through the production
// router; the Settings dialogs are part of the page bodies.
func TestScreens_ShowNoIssuePRorFRNumbers(t *testing.T) {
	engine := newEngine(t, secrets{full: true})

	targets := []string{
		"/", "/home/trade-status", "/home/pnl", "/home/positions", "/home/attention", "/home/details", "/home/attention-badge",
		"/scanner", "/scanner/scan", "/watchlist", "/symbols/7203",
		"/order-proposals", "/performance", "/calibration", "/activity",
		"/setup", settingspages.BasePath,
		"/settings/mcp-panel", "/settings/jev-prompt-panel", "/system/update-panel", "/system/status",
	}
	for _, page := range settingspages.Pages() {
		targets = append(targets, page.Path())
	}

	for _, target := range targets {
		rec := get(engine, target)
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", target, rec.Code)
			continue
		}
		text := visibleText(rec.Body.String())
		for _, m := range devNotePattern.FindAllStringIndex(text, -1) {
			from := max(m[0]-30, 0)
			to := min(m[1]+30, len(text))
			t.Errorf("GET %s shows a development reference %q: …%s…", target, text[m[0]:m[1]], strings.ReplaceAll(text[from:to], "\n", " "))
		}
	}
}
