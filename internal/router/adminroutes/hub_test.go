package adminroutes_test

import (
	"net/http"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/web/adminhub"
)

// hubTargets returns the Settings dialog ids the hub links to (`/settings#<id>`
// entries) and the other page paths it links to (fragment stripped).
func hubTargets() (dialogs []string, pages []string) {
	for _, entry := range adminhub.Entries() {
		path, fragment, _ := strings.Cut(entry.Href, "#")
		switch {
		case path == "/settings" && fragment != "":
			dialogs = append(dialogs, fragment)
		case !slices.Contains(pages, path):
			pages = append(pages, path)
		}
	}
	return dialogs, pages
}

func renderedDialogIDs(t *testing.T) []string {
	t.Helper()
	rec := get(newEngine(t, secrets{full: true}), "/settings")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /settings = %d, want 200", rec.Code)
	}
	var ids []string
	for _, m := range dialogIDPattern.FindAllStringSubmatch(rec.Body.String(), -1) {
		ids = append(ids, m[1])
	}
	if len(ids) == 0 {
		t.Fatal("rendered Settings page has no dialog")
	}
	return ids
}

// Nothing the Settings screen offers may be missing from the hub (issue #793
// 漏れなし): every card/dialog of the rendered page is the target of a hub
// entry. Adding a Settings card without a hub entry fails here.
func TestHub_ListsEverySettingsDialog(t *testing.T) {
	hubDialogs, _ := hubTargets()
	// A duplicated dialog id would make one of its cards unreachable by hash.
	seen := map[string]bool{}
	for _, id := range renderedDialogIDs(t) {
		if seen[id] {
			t.Errorf("Settings page renders dialog id %q more than once; hub links cannot tell the dialogs apart", id)
		}
		seen[id] = true
		if !slices.Contains(hubDialogs, id) {
			t.Errorf("Settings dialog %q has no admin hub entry (/settings#%s)", id, id)
		}
	}
}

// Every hub link into Settings must land on a dialog that exists, so a
// renamed or removed card cannot leave a dead hub link.
func TestHub_SettingsLinksTargetExistingDialogs(t *testing.T) {
	rendered := renderedDialogIDs(t)
	hubDialogs, _ := hubTargets()
	for _, id := range hubDialogs {
		if !slices.Contains(rendered, id) {
			t.Errorf("hub links to /settings#%s but the Settings page renders no such dialog (have %v)", id, rendered)
		}
	}
}

// The MCP card is added by issue #792; the hub entry must point at its
// dialog (`modal-mcp`) and that dialog must exist when MCP settings are wired.
func TestHub_MCPEntryOpensTheMCPDialog(t *testing.T) {
	var found bool
	for _, entry := range adminhub.Entries() {
		if entry.Name == "MCP" {
			found = true
			if entry.Href != "/settings#modal-mcp" {
				t.Errorf("MCP entry href = %q, want /settings#modal-mcp", entry.Href)
			}
		}
	}
	if !found {
		t.Fatal("hub has no MCP entry")
	}
	if !slices.Contains(renderedDialogIDs(t), "modal-mcp") {
		t.Error("Settings page renders no modal-mcp dialog")
	}
}

// A hub link with a `#fragment` onto a page (Kill Switch, バックテスト) must
// find that element on the page it links to.
func TestHub_PageAnchorsExistOnTheirPages(t *testing.T) {
	engine := newEngine(t, secrets{full: true})
	for _, entry := range adminhub.Entries() {
		path, fragment, ok := strings.Cut(entry.Href, "#")
		if !ok || path == "/settings" {
			continue
		}
		rec := get(engine, path)
		if rec.Code != http.StatusOK {
			t.Errorf("%s: GET %s = %d, want 200", entry.Name, path, rec.Code)
			continue
		}
		if !regexp.MustCompile(`\bid="` + regexp.QuoteMeta(fragment) + `"`).MatchString(rec.Body.String()) {
			t.Errorf("%s: %s renders no element id=%q for the hub link %s", entry.Name, path, fragment, entry.Href)
		}
	}
}

// Every main-nav entry (except 管理 itself) is reachable from the hub.
func TestHub_ListsEveryMainNavEntry(t *testing.T) {
	engine := newEngine(t, secrets{full: true})
	_, hubPages := hubTargets()
	for _, link := range mainNav(t, get(engine, "/admin").Body.String()) {
		if link.Href == "/admin" {
			continue
		}
		if !slices.Contains(hubPages, link.Href) {
			t.Errorf("main-nav entry %q (%s) is not listed in the admin hub", link.Label, link.Href)
		}
	}
}

// Every hub-listed page that renders the shared header links back to /admin,
// and 管理 is marked current only on /admin. /setup (first-run, no header)
// and /swagger (a static docs shell) are the documented exceptions.
func TestHub_EveryListedPageLinksBackToAdmin(t *testing.T) {
	engine := newEngine(t, secrets{full: true})
	_, hubPages := hubTargets()
	for _, path := range append(hubPages, "/admin", "/symbols/7203") {
		if path == "/setup" || path == "/swagger" {
			continue
		}
		rec := get(engine, path)
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", path, rec.Code)
			continue
		}
		var admin *navLink
		for _, link := range mainNav(t, rec.Body.String()) {
			if link.Href == "/admin" {
				admin = &link
			}
		}
		switch {
		case admin == nil:
			t.Errorf("%s has no main-nav link back to /admin", path)
		case admin.Label != "管理":
			t.Errorf("%s: /admin nav label = %q, want 管理", path, admin.Label)
		case admin.Current != (path == "/admin"):
			t.Errorf("%s: 管理 aria-current = %v, want %v", path, admin.Current, path == "/admin")
		}
	}
}

// Symbol Detail is parameterised (`/symbols/:symbol`, reached from Scanner)
// so it is in the spec table but not a hub link; it must not appear as one.
func TestHub_DoesNotLinkParameterisedPages(t *testing.T) {
	for _, entry := range adminhub.Entries() {
		if strings.Contains(entry.Href, "/symbols/") {
			t.Errorf("hub entry %q links to the parameterised %s", entry.Name, entry.Href)
		}
	}
}
