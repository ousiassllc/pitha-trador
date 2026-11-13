package settingsroutes_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/web/settingspages"
)

// legacyDialogPages is where every dialog lived on origin/main (before issue
// #895), by the old page id. Old bookmarks `/settings/<old page>#<dialog>`
// (docs/components/screens.md listed them as formal URLs) must still open the
// dialog: a dialog that stayed on its page is opened by that page itself, one
// that moved is forwarded by the page's own hash-route map.
var legacyDialogPages = map[string][]string{
	"operations":    {"modal-screener", "modal-policy-long", "modal-policy-short", "modal-snapshot-age", "modal-paper-bootstrap", "modal-sleep-guard"},
	"results":       {"modal-triple_barrier", "modal-metalabel", "modal-regime", "modal-orderflow"},
	"risk":          {"modal-paper-risk", "modal-daily-guard", "modal-trading_cost"},
	"asset-buckets": {"modal-asset-buckets"},
	"broker":        {"modal-broker", "modal-kabu", "modal-tachibana", "modal-tachibana-conn", "modal-tachibana-source", "modal-tachibana-nightly", "modal-tachibana-intraday", "modal-tachibana-screen"},
	"integrations":  {"modal-jev", "modal-luna", "modal-sol", "modal-opus", "modal-news-feed", "modal-slack", "modal-mcp", "modal-jev-prompt"},
	"maintenance":   {"modal-update", "modal-error-log", "modal-data-export", "modal-backup", "modal-logdir", "modal-logsettings"},
}

func TestLegacyDialogLinks_StillOpenTheirDialog(t *testing.T) {
	engine := newEngine(t, secrets{full: true})
	rendered := renderedDialogs(t)
	for oldPage, modals := range legacyDialogPages {
		oldPath := settingspages.BasePath + "/" + oldPage
		if oldPage == "asset-buckets" {
			if oldPath != settingspages.LegacyBucketsPath {
				t.Fatalf("legacy bucket path = %q", settingspages.LegacyBucketsPath)
			}
			// The 301 keeps the URL fragment, so the risk page opens the dialog.
			oldPath = settingspages.BasePath + "/" + settingspages.PageRisk
		}
		for _, modal := range modals {
			owner, ok := settingspages.PageOfTarget(modal)
			if !ok {
				t.Errorf("%s#%s: no page holds the dialog any more", oldPath, modal)
				continue
			}
			if !slices.Contains(rendered[owner.ID], modal) {
				t.Errorf("%s#%s: %s does not render the dialog", oldPath, modal, owner.Path())
			}
			if owner.Path() == oldPath {
				continue
			}
			// The dialog moved: the old page must forward the hash.
			body := get(engine, oldPath).Body.String()
			want := `&#34;` + modal + `&#34;:&#34;` + owner.Path() + `&#34;`
			if !strings.Contains(body, "data-hash-routes") || !strings.Contains(body, want) {
				t.Errorf("%s#%s: the page does not embed the hash route %s", oldPath, modal, want)
			}
			if !strings.Contains(body, "/static/dist/js/settings-hub/pitha-settings-hub.js") {
				t.Errorf("%s#%s: the page does not load the forwarding script", oldPath, modal)
			}
		}
	}
}

// Every Settings page, not only the hub, embeds the hash routes and loads the
// forwarding script.
func TestPages_EmbedHashRoutesAndForwardingScript(t *testing.T) {
	engine := newEngine(t, secrets{full: true})
	for _, page := range settingspages.Pages() {
		body := get(engine, page.Path()).Body.String()
		for _, want := range []string{"data-hash-routes", "&#34;modal-sleep-guard&#34;:&#34;/settings/maintenance&#34;", "/static/dist/js/settings-hub/pitha-settings-hub.js"} {
			if !strings.Contains(body, want) {
				t.Errorf("%s lacks %q", page.Path(), want)
			}
		}
	}
}
