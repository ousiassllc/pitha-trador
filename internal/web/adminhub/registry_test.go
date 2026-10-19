package adminhub_test

import (
	"net/url"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/web/adminhub"
)

// The categories are exactly the ones the issue decided, in that order.
func TestCategories_AreTheDeclaredList(t *testing.T) {
	var got []string
	for _, c := range adminhub.Categories() {
		got = append(got, c.Name)
	}
	want := []string{"運用", "成績・検証", "リスク・資金設定", "ブローカー・データ接続", "外部連携", "ログ・エクスポート"}
	if strings.Join(got, "/") != strings.Join(want, "/") {
		t.Errorf("categories = %v, want %v", got, want)
	}
}

// Registry integrity: no duplicates, nothing blank, every entry in a
// declared category, every href an in-app path.
func TestEntries_Integrity(t *testing.T) {
	categories := map[string]bool{}
	for _, c := range adminhub.Categories() {
		if c.ID == "" || c.Name == "" || c.Summary == "" {
			t.Errorf("category %+v has a blank field", c)
		}
		if categories[c.ID] {
			t.Errorf("duplicate category id %q", c.ID)
		}
		categories[c.ID] = true
	}
	names, hrefs := map[string]bool{}, map[string]bool{}
	used := map[string]bool{}
	for _, e := range adminhub.Entries() {
		if strings.TrimSpace(e.Name) == "" || strings.TrimSpace(e.Description) == "" {
			t.Errorf("entry %+v has a blank name or description", e)
		}
		if !categories[e.Category] {
			t.Errorf("entry %q has undeclared category %q", e.Name, e.Category)
		}
		used[e.Category] = true
		if names[e.Name] {
			t.Errorf("duplicate entry name %q", e.Name)
		}
		names[e.Name] = true
		if hrefs[e.Href] {
			t.Errorf("duplicate entry href %q (%s)", e.Href, e.Name)
		}
		hrefs[e.Href] = true
		u, err := url.Parse(e.Href)
		if err != nil || !strings.HasPrefix(e.Href, "/") || strings.HasPrefix(e.Href, "//") || u.Host != "" {
			t.Errorf("entry %q href %q is not an in-app path", e.Name, e.Href)
		}
	}
	for id := range categories {
		if !used[id] {
			t.Errorf("category %q has no entry", id)
		}
	}
}

// Visible keeps category order and hides the Swagger entry unless enabled.
func TestVisible_SwaggerEntryOnlyWhenEnabled(t *testing.T) {
	hasSwagger := func(swagger bool) bool {
		for _, s := range adminhub.Visible(swagger) {
			for _, e := range s.Entries {
				if e.Href == "/swagger" {
					return true
				}
			}
		}
		return false
	}
	if hasSwagger(false) {
		t.Error("/swagger is listed while Swagger is disabled")
	}
	if !hasSwagger(true) {
		t.Error("/swagger is not listed while Swagger is enabled")
	}
	sections := adminhub.Visible(false)
	cats := adminhub.Categories()
	if len(sections) != len(cats) {
		t.Fatalf("got %d sections, want %d", len(sections), len(cats))
	}
	for i, s := range sections {
		if s.Category != cats[i] {
			t.Errorf("section %d = %q, want %q", i, s.Category.Name, cats[i].Name)
		}
	}
}
