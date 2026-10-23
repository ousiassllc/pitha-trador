package settingsroutes_test

import (
	"html"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/opsettings"
	"github.com/ousiassllc/pitha-trador/internal/web/settingspages"
)

// renderedDialogs returns the dialog ids each Settings page renders, by page id.
func renderedDialogs(t *testing.T) map[string][]string {
	t.Helper()
	engine := newEngine(t, secrets{full: true})
	out := map[string][]string{}
	for _, page := range settingspages.Pages() {
		rec := get(engine, page.Path())
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s = %d, want 200", page.Path(), rec.Code)
		}
		for _, m := range dialogIDPattern.FindAllStringSubmatch(rec.Body.String(), -1) {
			out[page.ID] = append(out[page.ID], m[1])
		}
	}
	return out
}

// Nothing the Settings screens offer may be missing from the pages (issue
// #793 漏れなし, now per page): every dialog a page renders is the one the
// registry places on that page, and every registered dialog is rendered by
// its page. Adding a Settings card without placing it fails here; so does a
// dialog id rendered twice (a hash link could not tell them apart).
func TestPages_RenderExactlyTheDialogsTheRegistryPlacesOnThem(t *testing.T) {
	rendered := renderedDialogs(t)
	seen := map[string]string{}
	for _, page := range settingspages.Pages() {
		var want []string
		for _, d := range settingspages.DialogsOf(page.ID) {
			want = append(want, d.ModalID)
		}
		got := rendered[page.ID]
		for _, id := range got {
			if other, dup := seen[id]; dup {
				t.Errorf("dialog %q is rendered on both %s and %s", id, other, page.ID)
			}
			seen[id] = page.ID
			if !slices.Contains(want, id) {
				t.Errorf("%s renders dialog %q that the registry does not place on it", page.Path(), id)
			}
		}
		for _, id := range want {
			if !slices.Contains(got, id) {
				t.Errorf("%s does not render dialog %q the registry places on it (have %v)", page.Path(), id, got)
			}
		}
	}
}

// Every operational setting key (opsettings.Keys()) has its editable row on
// exactly one Settings page.
func TestPages_EveryOperationalSettingIsEditableOnExactlyOnePage(t *testing.T) {
	engine := newEngine(t, secrets{full: true})
	counts := map[string]int{}
	for _, page := range settingspages.Pages() {
		body := get(engine, page.Path()).Body.String()
		for _, key := range opsettings.Keys() {
			counts[key] += strings.Count(body, `data-testid="setting-field-row-`+key+`"`)
		}
	}
	for _, key := range opsettings.Keys() {
		if counts[key] != 1 {
			t.Errorf("operational setting %q has %d editable rows across the Settings pages, want exactly 1", key, counts[key])
		}
	}
}

// `/settings` lists every page as a link to its path, with the registry's
// name.
func TestHub_ListsEveryPage(t *testing.T) {
	body := get(newEngine(t, secrets{full: true}), "/settings").Body.String()
	for _, page := range settingspages.Pages() {
		if !strings.Contains(body, `data-testid="settings-link-page-`+page.ID+`"`) {
			t.Errorf("hub has no entry for page %q", page.ID)
		}
		if !strings.Contains(body, `href="`+page.Path()+`"`) {
			t.Errorf("hub does not link to %s", page.Path())
		}
		if !strings.Contains(body, page.Name) {
			t.Errorf("hub does not show the name %q", page.Name)
		}
	}
}

// The hub shows the six pages of issue #895 and, under them, the screens
// outside the main menu (稼働ログ, 初回セットアップ).
func TestHub_ShowsSixPagesAndTheLinksOutsideTheMenu(t *testing.T) {
	body := get(newEngine(t, secrets{full: true}), "/settings").Body.String()
	if n := len(regexp.MustCompile(`data-testid="settings-link-page-`).FindAllString(body, -1)); n != 6 {
		t.Errorf("hub lists %d pages, want 6", n)
	}
	for _, want := range []string{`data-testid="settings-hub-links"`, `href="/activity"`, ">稼働ログ<", `href="/setup"`} {
		if !strings.Contains(body, want) {
			t.Errorf("hub lacks %q", want)
		}
	}
}

// The MCP card is added by issue #792; its dialog is on the 外部連携 page.
func TestPages_MCPDialogIsOnTheIntegrationsPage(t *testing.T) {
	if !slices.Contains(renderedDialogs(t)[settingspages.PageIntegrations], "modal-mcp") {
		t.Error("the integrations page renders no modal-mcp dialog")
	}
}

// A link with a `#fragment` onto a page must find that element on the page it
// links to.
func TestPages_LinkAnchorsExistOnTheirPages(t *testing.T) {
	engine := newEngine(t, secrets{full: true})
	for _, link := range settingspages.AllLinks() {
		path, fragment, ok := strings.Cut(link.Href, "#")
		if path == "/swagger" || path == "/setup" {
			continue
		}
		rec := get(engine, path)
		if rec.Code != http.StatusOK {
			t.Errorf("%s: GET %s = %d, want 200", link.Name, path, rec.Code)
			continue
		}
		if ok && !regexp.MustCompile(`\bid="`+regexp.QuoteMeta(fragment)+`"`).MatchString(rec.Body.String()) {
			t.Errorf("%s: %s renders no element id=%q for the link %s", link.Name, path, fragment, link.Href)
		}
	}
}

// anchorHrefs returns the in-app targets (path and query, no fragment) of every
// `<a href>` in body.
func anchorHrefs(body string) []string {
	var out []string
	for _, m := range regexp.MustCompile(`<a\b[^>]*\bhref="([^"]+)"`).FindAllStringSubmatch(body, -1) {
		href, _, _ := strings.Cut(html.UnescapeString(m[1]), "#")
		if strings.HasPrefix(href, "/") && !strings.HasPrefix(href, "//") && href != "" {
			out = append(out, href)
		}
	}
	return out
}

// Every page that existed before the menu was reorganised (issue #895) opens
// within two clicks of the main menu: the nav entry is click one, a link,
// tab or card on that page is click two. The parameterised pages
// (`/symbols/:symbol`, `/order-proposals/:id`) are reached from their list
// page's rows, which this empty fixture has none of.
func TestMenu_EveryPageIsReachableWithinTwoClicks(t *testing.T) {
	engine := newEngine(t, secrets{full: true})
	start := get(engine, "/scanner").Body.String()
	reachable := map[string]bool{}
	var nav []string
	for _, link := range mainNav(t, start) {
		nav = append(nav, link.Href)
		reachable[link.Href] = true
	}
	for _, href := range nav {
		rec := get(engine, href)
		if rec.Code != http.StatusOK {
			continue // "/" redirects until the Home page exists
		}
		for _, next := range anchorHrefs(rec.Body.String()) {
			reachable[next] = true
		}
	}

	want := []string{
		"/", "/scanner", "/watchlist", "/order-proposals", "/performance", "/performance?tab=backtest", "/calibration",
		"/activity", "/settings", "/setup",
	}
	for _, page := range settingspages.Pages() {
		want = append(want, page.Path())
	}
	for _, href := range want {
		if !reachable[href] {
			t.Errorf("%s is not reachable within two clicks of the main menu", href)
		}
		if path, _, _ := strings.Cut(href, "?"); path != "/" && get(engine, href).Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", href, get(engine, href).Code)
		}
	}
}

// The main menu has exactly the five entries and marks the one the page
// belongs to (issue #895): 候補銘柄 for the Scanner, the 監視リスト and a
// symbol's detail; 成績 for 実績, バックテスト and 予測の精度; 設定 for 稼働ログ
// and every Settings page; the error page marks nothing.
func TestNav_MarksTheEntryEveryPageBelongsTo(t *testing.T) {
	engine := newEngine(t, secrets{full: true})
	pages := map[string]string{
		"/scanner":                  "/scanner",
		"/watchlist":                "/scanner",
		"/symbols/7203":             "/scanner",
		"/order-proposals":          "/order-proposals",
		"/performance":              "/performance",
		"/performance?tab=backtest": "/performance",
		"/performance?from=2026-09-01&to=2026-09-10": "/performance",
		"/calibration": "/performance",
		"/activity":    "/settings",
		"/settings":    "/settings",
	}
	for _, page := range settingspages.Pages() {
		pages[page.Path()] = "/settings"
	}
	wantLabels := []string{"ホーム", "候補銘柄", "AIの注文提案", "成績", "設定"}
	for path, current := range pages {
		rec := get(engine, path)
		if rec.Code != http.StatusOK && rec.Code != http.StatusServiceUnavailable && rec.Code != http.StatusInternalServerError {
			t.Errorf("GET %s = %d", path, rec.Code)
			continue
		}
		var labels, marked []string
		for _, link := range mainNav(t, rec.Body.String()) {
			labels = append(labels, link.Label)
			if link.Current {
				marked = append(marked, link.Href)
			}
		}
		if strings.Join(labels, " ") != strings.Join(wantLabels, " ") {
			t.Errorf("%s: nav labels = %v, want %v", path, labels, wantLabels)
		}
		if len(marked) != 1 || marked[0] != current {
			t.Errorf("%s: nav marks %v, want only %s", path, marked, current)
		}
	}
}

// Each page links back to the hub so the Settings tree can be walked in both
// directions.
func TestPages_LinkBackToTheHub(t *testing.T) {
	engine := newEngine(t, secrets{full: true})
	for _, page := range settingspages.Pages() {
		if body := get(engine, page.Path()).Body.String(); !strings.Contains(body, `data-testid="settings-breadcrumb"`) || !strings.Contains(body, `href="/settings"`) {
			t.Errorf("%s has no breadcrumb back to /settings", page.Path())
		}
	}
}

// Symbol Detail is parameterised (`/symbols/:symbol`, reached from Scanner),
// so it is never a Settings link.
func TestPages_DoNotLinkParameterisedPages(t *testing.T) {
	for _, link := range settingspages.AllLinks() {
		if strings.Contains(link.Href, "/symbols/") {
			t.Errorf("link %q points to the parameterised %s", link.Name, link.Href)
		}
	}
}

// The hub embeds the hash-route map that forwards old `/settings#modal-<id>`
// links, and the old header version link target resolves to the maintenance
// page.
func TestHub_EmbedsHashRoutesForOldLinks(t *testing.T) {
	body := get(newEngine(t, secrets{full: true}), "/settings").Body.String()
	for _, want := range []string{"data-hash-routes", "&#34;modal-jev&#34;:&#34;/settings/integrations&#34;", "&#34;update-panel&#34;:&#34;/settings/maintenance&#34;"} {
		if !strings.Contains(body, want) {
			t.Errorf("hub lacks %q", want)
		}
	}
	if !strings.Contains(body, "/static/dist/js/settings-hub/pitha-settings-hub.js") {
		t.Error("hub does not load the hash-forwarding script")
	}
}
