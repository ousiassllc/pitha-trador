package settingsroutes_test

import (
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

// The MCP card is added by issue #792; its dialog is on the 外部連携 page.
func TestPages_MCPDialogIsOnTheIntegrationsPage(t *testing.T) {
	if !slices.Contains(renderedDialogs(t)[settingspages.PageIntegrations], "modal-mcp") {
		t.Error("the integrations page renders no modal-mcp dialog")
	}
}

// A link with a `#fragment` onto a page (Kill Switch, バックテスト) must find
// that element on the page it links to.
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

// Every main-nav entry other than Settings is reachable from a Settings
// page's links (the former admin hub's job).
func TestPages_EveryMainNavEntryIsReachableFromSettings(t *testing.T) {
	engine := newEngine(t, secrets{full: true})
	var linked []string
	for _, link := range settingspages.AllLinks() {
		path, _, _ := strings.Cut(link.Href, "#")
		linked = append(linked, path)
	}
	for _, link := range mainNav(t, get(engine, "/settings").Body.String()) {
		if link.Href == "/settings" {
			continue
		}
		if !slices.Contains(linked, link.Href) {
			t.Errorf("main-nav entry %q (%s) is not linked from any Settings page", link.Label, link.Href)
		}
	}
}

// The menu has exactly one Settings entry: labelled "Settings", current on
// the hub and on every page under it, and not current elsewhere. No
// standalone 管理 entry or /admin link remains.
func TestNav_HasSingleSettingsEntryOnEveryPage(t *testing.T) {
	engine := newEngine(t, secrets{full: true})
	targets := []string{"/scanner", "/watchlist", "/performance", "/calibration", "/activity", "/symbols/7203", "/settings"}
	for _, page := range settingspages.Pages() {
		targets = append(targets, page.Path())
	}
	for _, path := range targets {
		rec := get(engine, path)
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", path, rec.Code)
			continue
		}
		var settings []navLink
		for _, link := range mainNav(t, rec.Body.String()) {
			if link.Label == "管理" || link.Href == "/admin" {
				t.Errorf("%s: nav still has a 管理 entry: %+v", path, link)
			}
			if link.Href == "/settings" {
				settings = append(settings, link)
			}
		}
		if len(settings) != 1 {
			t.Errorf("%s: nav has %d Settings entries, want 1", path, len(settings))
			continue
		}
		wantCurrent := strings.HasPrefix(path, "/settings")
		if settings[0].Label != "Settings" || settings[0].Current != wantCurrent {
			t.Errorf("%s: Settings nav = %+v, want label Settings, current %v", path, settings[0], wantCurrent)
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
