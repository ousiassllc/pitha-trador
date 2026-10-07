package organismstest

import (
	"regexp"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/web/organisms"
)

func containerTag(t *testing.T, html, id string) string {
	t.Helper()
	tag := regexp.MustCompile(`(?s)<div\b[^>]*\bid="` + id + `"[^>]*>`).FindString(html)
	if tag == "" {
		t.Fatalf("no #%s in header: %s", id, html)
	}
	return tag
}

// The three banner frames share one placement convention (issue #625): a
// full-width last row that collapses while empty.
func TestHeader_BannerFramesShareOneConvention(t *testing.T) {
	html := renderComponent(t, organisms.Header(domain.SystemState(""), organisms.NavNone))
	for _, id := range []string{"config-banner", "update-banner", "marketdata-banner"} {
		tag := containerTag(t, html, id)
		for _, c := range []string{"order-last", "w-full", "empty:hidden"} {
			if !strings.Contains(tag, c) {
				t.Errorf("#%s lacks %q: %s", id, c, tag)
			}
		}
	}
}

// Polled banners sit in a fixed polite live region on the container, so a
// poll re-rendering the same banner is not re-announced as an alert
// (issue #626); the swap-skipping marker lets the client drop unchanged
// responses.
func TestHeader_PolledBannersAreFixedLiveRegions(t *testing.T) {
	html := renderComponent(t, organisms.Header(domain.SystemState(""), organisms.NavNone))
	for _, id := range []string{"update-banner", "marketdata-banner"} {
		tag := containerTag(t, html, id)
		for _, want := range []string{`role="status"`, `aria-live="polite"`, "data-live-banner"} {
			if !strings.Contains(tag, want) {
				t.Errorf("#%s lacks %q: %s", id, want, tag)
			}
		}
	}
}

func TestBanners_BodiesCarryNoRole(t *testing.T) {
	market := renderComponent(t, organisms.MarketDataBanner("bad_password", "KABU_API_PASSWORD を確認してください。"))
	update := renderComponent(t, organisms.UpdateBanner(organisms.UpdateBannerProps{Available: true, Version: "v9.9.9"}))
	for name, html := range map[string]string{"MarketDataBanner": market, "UpdateBanner": update} {
		if !strings.Contains(html, "data-testid=") {
			t.Fatalf("%s rendered nothing: %s", name, html)
		}
		if strings.Contains(html, "role=") {
			t.Errorf("%s body must not carry its own role (the container is the live region): %s", name, html)
		}
	}
}

func TestSecretsBanner_HasNoOwnMargin(t *testing.T) {
	html := renderComponent(t, organisms.SecretsBanner([]string{"SLACK_WEBHOOK_URL"}))
	if strings.Contains(html, "mb-4") {
		t.Errorf("banner placement belongs to #config-banner, not a margin: %s", html)
	}
}

// Only the current page's link carries aria-current (issue #631).
func TestMainNav_MarksOnlyCurrent(t *testing.T) {
	html := renderComponent(t, organisms.MainNav(organisms.NavCalibration))
	if got := strings.Count(html, `aria-current="page"`); got != 1 {
		t.Fatalf("aria-current count = %d, want 1: %s", got, html)
	}
	if !regexp.MustCompile(`<a[^>]*href="/calibration"[^>]*aria-current="page"[^>]*>Calibration</a>`).MatchString(html) {
		t.Errorf("aria-current not on Calibration link: %s", html)
	}
	if !regexp.MustCompile(`class="font-semibold text-slate-900"[^>]*href="/calibration"`).MatchString(html) {
		t.Errorf("current link not emphasised: %s", html)
	}
	if strings.Contains(renderComponent(t, organisms.MainNav(organisms.NavNone)), "aria-current") {
		t.Errorf("NavNone must mark nothing")
	}
}

// #config-banner refetches on the event Settings' Save/Delete responses
// fire (issue #695), like #update-banner does for updateStatusChanged; it
// is operator-initiated, so it carries no X-Pitha-Background header.
func TestHeader_ConfigBannerRefetchesOnSecretsStatusChanged(t *testing.T) {
	html := renderComponent(t, organisms.Header(domain.SystemState(""), organisms.NavNone))
	tag := containerTag(t, html, "config-banner")
	if !strings.Contains(tag, `hx-trigger="load, secretsStatusChanged from:body"`) {
		t.Errorf("#config-banner lacks the secretsStatusChanged trigger: %s", tag)
	}
	if strings.Contains(tag, "X-Pitha-Background") {
		t.Errorf("#config-banner refetch is operator-initiated, must not be background: %s", tag)
	}
}
