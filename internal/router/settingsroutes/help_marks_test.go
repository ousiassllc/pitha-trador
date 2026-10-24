package settingsroutes_test

import (
	"net/http"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

// Every help mark the app serves is usable: its text element has an id that is
// unique on the page (the button's aria-describedby points at it), the button
// has an accessible name, and the mark is not inside a label, button or link
// (a click or focus there would toggle the control instead of the help).
func TestScreens_HelpMarksAreWellFormed(t *testing.T) {
	engine := newEngine(t, secrets{full: true})

	for _, target := range screenTargets() {
		rec := get(engine, target)
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", target, rec.Code)
			continue
		}
		doc, err := html.Parse(strings.NewReader(rec.Body.String()))
		if err != nil {
			t.Errorf("GET %s: parse: %v", target, err)
			continue
		}
		ids := map[string]int{}
		collectIDs(doc, ids)
		checkHelpMarks(t, target, doc, ids)
	}
}

func attr(n *html.Node, key string) (string, bool) {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val, true
		}
	}
	return "", false
}

func collectIDs(n *html.Node, ids map[string]int) {
	if n.Type == html.ElementNode {
		if id, ok := attr(n, "id"); ok {
			ids[id]++
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		collectIDs(c, ids)
	}
}

func checkHelpMarks(t *testing.T, target string, n *html.Node, ids map[string]int) {
	t.Helper()
	if n.Type == html.ElementNode {
		if _, ok := attr(n, "data-help-mark-button"); ok {
			label, _ := attr(n, "aria-label")
			if strings.TrimSpace(label) == "" {
				t.Errorf("GET %s: a help mark button has no aria-label", target)
			}
			ref, _ := attr(n, "aria-describedby")
			if ids[ref] != 1 {
				t.Errorf("GET %s: help text id %q appears %d times, want exactly 1", target, ref, ids[ref])
			}
			for p := n.Parent; p != nil; p = p.Parent {
				if p.Type == html.ElementNode && (p.Data == "label" || p.Data == "button" || p.Data == "a" || p.Data == "summary") {
					t.Errorf("GET %s: help mark %q sits inside <%s>", target, ref, p.Data)
					break
				}
			}
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		checkHelpMarks(t, target, c, ids)
	}
}
