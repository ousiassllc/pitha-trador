package settingspages_test

import (
	"net/url"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/web/settingspages"
)

// The pages are the ones issue #805 proposed, in that order. A page added or
// renamed later must update this list on purpose.
func TestPages_AreTheDeclaredList(t *testing.T) {
	var got []string
	for _, p := range settingspages.Pages() {
		got = append(got, p.Name)
	}
	want := []string{"運用", "成績・検証", "リスク・資金", "ブローカー・データ", "外部連携", "ログ・エクスポート"}
	if strings.Join(got, "/") != strings.Join(want, "/") {
		t.Errorf("pages = %v, want %v", got, want)
	}
}

// Registry integrity: nothing blank, no duplicates, every dialog and link on
// a declared page, every page non-empty, every href an in-app path.
func TestRegistry_Integrity(t *testing.T) {
	declared := map[string]bool{}
	for _, p := range settingspages.Pages() {
		if p.ID == "" || p.Name == "" || p.Summary == "" || strings.ContainsAny(p.ID, "/#?: ") {
			t.Errorf("page %+v has a blank field or an id that is not a path segment", p)
		}
		if declared[p.ID] {
			t.Errorf("duplicate page id %q", p.ID)
		}
		declared[p.ID] = true
		if got := p.Path(); got != "/settings/"+p.ID {
			t.Errorf("page %q path = %q", p.ID, got)
		}
	}

	used := map[string]bool{}
	targets := map[string]bool{}
	for _, d := range settingspages.Dialogs() {
		if !declared[d.Page] {
			t.Errorf("dialog %q is on undeclared page %q", d.ModalID, d.Page)
		}
		used[d.Page] = true
		if !strings.HasPrefix(d.ModalID, "modal-") {
			t.Errorf("dialog id %q does not start with modal-", d.ModalID)
		}
		for _, id := range d.Targets() {
			if targets[id] {
				t.Errorf("hash target %q is claimed by more than one dialog", id)
			}
			targets[id] = true
		}
	}

	names, hrefs := map[string]bool{}, map[string]bool{}
	for _, l := range settingspages.AllLinks() {
		if strings.TrimSpace(l.Name) == "" || strings.TrimSpace(l.Description) == "" {
			t.Errorf("link %+v has a blank name or description", l)
		}
		if !declared[l.Page] {
			t.Errorf("link %q is on undeclared page %q", l.Name, l.Page)
		}
		used[l.Page] = true
		if names[l.Name] {
			t.Errorf("duplicate link name %q", l.Name)
		}
		names[l.Name] = true
		if hrefs[l.Href] {
			t.Errorf("duplicate link href %q (%s)", l.Href, l.Name)
		}
		hrefs[l.Href] = true
		u, err := url.Parse(l.Href)
		if err != nil || !strings.HasPrefix(l.Href, "/") || strings.HasPrefix(l.Href, "//") || u.Host != "" {
			t.Errorf("link %q href %q is not an in-app path", l.Name, l.Href)
		}
		if strings.HasPrefix(l.Href, settingspages.BasePath) {
			t.Errorf("link %q href %q points inside Settings; list it as a dialog or page instead", l.Name, l.Href)
		}
	}
	for id := range declared {
		if !used[id] {
			t.Errorf("page %q holds no dialog or link", id)
		}
	}
}

// LinksOf hides the Swagger link unless it is enabled; DialogsOf and LinksOf
// return only the page's own rows.
func TestLinksOf_SwaggerOnlyWhenEnabled(t *testing.T) {
	has := func(swagger bool) bool {
		for _, p := range settingspages.Pages() {
			for _, l := range settingspages.LinksOf(p.ID, swagger) {
				if l.Href == "/swagger" {
					return true
				}
			}
		}
		return false
	}
	if has(false) {
		t.Error("/swagger is listed while Swagger is disabled")
	}
	if !has(true) {
		t.Error("/swagger is not listed while Swagger is enabled")
	}
	for _, p := range settingspages.Pages() {
		for _, d := range settingspages.DialogsOf(p.ID) {
			if d.Page != p.ID {
				t.Errorf("DialogsOf(%q) returned %q of page %q", p.ID, d.ModalID, d.Page)
			}
		}
		for _, l := range settingspages.LinksOf(p.ID, true) {
			if l.Page != p.ID {
				t.Errorf("LinksOf(%q) returned %q of page %q", p.ID, l.Name, l.Page)
			}
		}
	}
}

// HashRoutes sends every dialog id and alias to the page holding it, so
// `/settings#modal-<id>` links from before the pages existed still open the
// dialog.
func TestHashRoutes_CoverEveryDialogAndAlias(t *testing.T) {
	routes := settingspages.HashRoutes()
	for _, d := range settingspages.Dialogs() {
		page, ok := settingspages.PageByID(d.Page)
		if !ok {
			t.Fatalf("dialog %q: unknown page %q", d.ModalID, d.Page)
		}
		for _, id := range d.Targets() {
			if routes[id] != page.Path() {
				t.Errorf("HashRoutes[%q] = %q, want %q", id, routes[id], page.Path())
			}
			if got, ok := settingspages.PageOfTarget(id); !ok || got.ID != page.ID {
				t.Errorf("PageOfTarget(%q) = %v, %v; want page %q", id, got, ok, page.ID)
			}
		}
	}
	if _, ok := settingspages.PageOfTarget("modal-does-not-exist"); ok {
		t.Error("PageOfTarget answered for an unknown id")
	}
}
