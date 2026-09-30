package layout_test

import (
	"context"
	"encoding/json"
	"html"
	"regexp"
	"strings"
	"testing"

	"github.com/a-h/templ"

	"github.com/ousiassllc/pitha-trador/internal/version"
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

// Every page shows the running build's version in the header and links it
// to Settings' アップデート section, but the header never carries the
// update-check button (a check that finds an installer restarts the app).
func TestShell_HeaderShowsVersionLinkedToSettingsUpdatePanel(t *testing.T) {
	original := version.Version
	version.Version = "v1.2.3"
	t.Cleanup(func() { version.Version = original })

	out := render(t, layout.Shell("t"))

	if !regexp.MustCompile(`<a[^>]*id="header-version"[^>]*href="/settings#update-panel"[^>]*>v1\.2\.3</a>`).MatchString(out) {
		t.Errorf("Shell header has no version link to /settings#update-panel; body=%s", out)
	}
	if strings.Contains(out, "/system/update-check") {
		t.Errorf("Shell must not carry the update-check button; body=%s", out)
	}
}

// Every page shows the app logo (issue #238) in the header, linking to the
// app's start page, ahead of the nav without displacing the version /
// StatusDot / Kill Switch items that follow it.
func TestShell_HeaderShowsLogoBeforeNav(t *testing.T) {
	out := render(t, layout.Shell("t"))

	logoLink := regexp.MustCompile(`(?s)<a\b[^>]*\bid="header-logo"[^>]*>.*?</a>`).FindString(out)
	if logoLink == "" {
		t.Fatalf("Shell header has no #header-logo link; body=%s", out)
	}
	if !regexp.MustCompile(`\bhref="/scanner"`).MatchString(logoLink) {
		t.Errorf("logo link must go to /scanner; link=%s", logoLink)
	}
	img := regexp.MustCompile(`<img\b[^>]*>`).FindString(logoLink)
	if !strings.Contains(img, `src="/static/img/logo.svg"`) || !strings.Contains(img, `alt=""`) {
		t.Errorf("logo must be /static/img/logo.svg with empty alt (the adjacent text names the app); img=%s", img)
	}
	if !strings.Contains(logoLink, "Pitha Trador") {
		t.Errorf("logo link must show the app name; link=%s", logoLink)
	}

	// Document order: logo, nav, then the justify-between items (version,
	// the StatusDot container, the Kill Switch panel) are all still there.
	last, lastName := -1, ""
	for _, marker := range []string{`id="header-logo"`, `<nav `, `id="header-version"`, `id="header-status"`, `<pitha-kill-switch-panel`} {
		at := strings.Index(out, marker)
		if at < 0 {
			t.Fatalf("Shell header lacks %s; body=%s", marker, out)
		}
		if at < last {
			t.Errorf("%s must come after %s in the header; body=%s", marker, lastName, out)
		}
		last, lastName = at, marker
	}
	if !regexp.MustCompile(`<nav\b[^>]*\baria-label="[^"]+"`).MatchString(out) {
		t.Errorf("header <nav> must carry an aria-label; body=%s", out)
	}
}
