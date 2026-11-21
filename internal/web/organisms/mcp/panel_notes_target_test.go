package mcp_test

import (
	"context"
	"strings"
	"testing"

	"golang.org/x/net/html"

	"github.com/ousiassllc/pitha-trador/internal/web/organisms/mcp"
)

// issue #1199: #mcp-panel (settings_modals.templ) carries hx-target="this", and
// htmx inherits hx-target down the tree, resolving "this" to the closest
// ancestor that has the attribute. The research-note slot must therefore name
// its own target, or its load response replaces the whole panel instead of
// filling the slot.
func TestResearchNotesSlotNamesItsOwnTarget(t *testing.T) {
	var sb strings.Builder
	props := mcp.PanelProps{WriteGate: true, ResearchNotes: true}
	if err := mcp.Panel(props).Render(context.Background(), &sb); err != nil {
		t.Fatal(err)
	}
	doc, err := html.Parse(strings.NewReader(sb.String()))
	if err != nil {
		t.Fatal(err)
	}
	slot := findByID(doc, "mcp-research-notes")
	if slot == nil {
		t.Fatal("#mcp-research-notes is not rendered")
	}
	attrs := map[string]string{}
	for _, a := range slot.Attr {
		attrs[a.Key] = a.Val
	}
	if got := attrs["hx-target"]; got != "this" {
		t.Errorf(`#mcp-research-notes hx-target = %q, want "this" (it would inherit #mcp-panel's)`, got)
	}
	if got := attrs["hx-get"]; got != "/settings/mcp/research-notes" {
		t.Errorf("#mcp-research-notes hx-get = %q", got)
	}
}

func findByID(n *html.Node, id string) *html.Node {
	if n.Type == html.ElementNode {
		for _, a := range n.Attr {
			if a.Key == "id" && a.Val == id {
				return n
			}
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if found := findByID(c, id); found != nil {
			return found
		}
	}
	return nil
}
