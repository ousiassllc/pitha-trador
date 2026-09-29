package layout_test

import (
	"context"
	"encoding/json"
	"html"
	"regexp"
	"strings"
	"testing"

	"github.com/a-h/templ"

	"github.com/ousiassllc/pitha-trador/internal/web/layout"
)

func render(t *testing.T, c templ.Component) string {
	t.Helper()
	var sb strings.Builder
	if err := c.Render(context.Background(), &sb); err != nil {
		t.Fatalf("Render: %v", err)
	}
	return sb.String()
}

var htmxConfigRe = regexp.MustCompile(`<meta name="htmx-config" content="([^"]*)"`)

// Both shells must carry the whole error-feedback wiring (issues #110/#121):
// htmx swaps 4xx/5xx into `#toast-region` only via this config, and the
// generic-toast script clones `#toast-template`.
func TestShells_WireHTMXErrorToasts(t *testing.T) {
	for name, page := range map[string]templ.Component{
		"Shell":      layout.Shell("t"),
		"SetupShell": layout.SetupShell("t"),
	} {
		out := render(t, page)

		m := htmxConfigRe.FindStringSubmatch(out)
		if m == nil {
			t.Fatalf("%s: no htmx-config meta in %s", name, out)
		}
		var cfg struct {
			ResponseHandling []struct {
				Code         string `json:"code"`
				Swap         bool   `json:"swap"`
				Error        bool   `json:"error"`
				Target       string `json:"target"`
				SwapOverride string `json:"swapOverride"`
			} `json:"responseHandling"`
		}
		if err := json.Unmarshal([]byte(html.UnescapeString(m[1])), &cfg); err != nil {
			t.Fatalf("%s: htmx-config is not valid JSON: %v", name, err)
		}
		var found bool
		for _, r := range cfg.ResponseHandling {
			if r.Code == "[45].." {
				found = r.Swap && r.Error && r.Target == "#toast-region" && r.SwapOverride == "beforeend"
			}
		}
		if !found {
			t.Errorf("%s: htmx-config = %+v, want 4xx/5xx swapped (error) into #toast-region beforeend", name, cfg)
		}

		for _, want := range []string{
			`id="toast-region"`,
			`id="toast-template"`,
			`/static/dist/js/htmx-errors/pitha-htmx-errors.js`,
		} {
			if !strings.Contains(out, want) {
				t.Errorf("%s: output lacks %q", name, want)
			}
		}
	}
}
