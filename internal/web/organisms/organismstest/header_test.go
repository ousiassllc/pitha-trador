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

// The five banners that stacked under the header on every page (unset keys,
// update, connection, daily halt, health alerts) are condensed into one badge
// container (issue #896); none of their containers is rendered.
func TestHeader_BannersAreCondensedIntoTheAttentionBadge(t *testing.T) {
	html := renderComponent(t, organisms.Header(domain.SystemState(""), organisms.NavScanner))
	for _, id := range []string{"config-banner", "update-banner", "marketdata-banner", "daily-halt-banner", "health-alert-banner"} {
		if strings.Contains(html, `id="`+id+`"`) {
			t.Errorf("#%s is still rendered in the header: the badge replaces it", id)
		}
	}
	tag := containerTag(t, html, "attention-badge")
	// It is a polite live region that collapses while empty; the swap-skipping
	// marker lets the client drop unchanged responses (issue #626); the 30s poll
	// is a background request (it must not refresh the operator heartbeat), and
	// Settings' Save/Delete and the manual update check refresh it at once.
	for _, want := range []string{
		`role="status"`, `aria-live="polite"`, "data-live-banner", "empty:hidden",
		`hx-get="/home/attention-badge"`, `X-Pitha-Background`,
		`hx-trigger="load, every 30s, secretsStatusChanged from:body, updateStatusChanged from:body"`,
	} {
		if !strings.Contains(tag, want) {
			t.Errorf("#attention-badge lacks %q: %s", want, tag)
		}
	}
}

// The Home screen lists every alert in full, so its header carries no badge
// and the Kill Switch panel (rendered by the 取引の状態 card instead) appears
// once; every other page keeps the panel in the header.
func TestHeader_HomeHasNoBadgeAndNoKillSwitchPanel(t *testing.T) {
	home := renderComponent(t, organisms.Header(domain.SystemStateRunning, organisms.NavHome))
	if strings.Contains(home, "attention-badge") || strings.Contains(home, "<pitha-kill-switch-panel") {
		t.Errorf("the Home header must carry neither the badge nor the Kill Switch panel: %s", home)
	}
	if !strings.Contains(home, `id="header-status"`) {
		t.Errorf("the Home header keeps the status badge: %s", home)
	}
	other := renderComponent(t, organisms.Header(domain.SystemStateRunning, organisms.NavScanner))
	if !strings.Contains(other, "<pitha-kill-switch-panel") || !strings.Contains(other, `id="attention-badge"`) {
		t.Errorf("another page's header keeps the Kill Switch panel and the badge: %s", other)
	}
}

// 「要対応 N 件」 links to Home and shows the count; nothing for 0.
func TestAttentionBadge_CountAndLink(t *testing.T) {
	html := renderComponent(t, organisms.AttentionBadge(3))
	if !regexp.MustCompile(`<a[^>]*href="/"[^>]*data-testid="attention-badge"[^>]*data-count="3"[^>]*>要対応 3 件</a>`).MatchString(html) {
		t.Errorf("badge = %s", html)
	}
	if got := strings.TrimSpace(renderComponent(t, organisms.AttentionBadge(0))); got != "" {
		t.Errorf("no items must render nothing, got %q", got)
	}
}

// Only the current page's link carries aria-current (issue #631).
func TestMainNav_MarksOnlyCurrent(t *testing.T) {
	html := renderComponent(t, organisms.MainNav(organisms.NavPerformance))
	if got := strings.Count(html, `aria-current="page"`); got != 1 {
		t.Fatalf("aria-current count = %d, want 1: %s", got, html)
	}
	if !regexp.MustCompile(`<a[^>]*href="/performance"[^>]*aria-current="page"[^>]*>成績</a>`).MatchString(html) {
		t.Errorf("aria-current not on 成績 link: %s", html)
	}
	if !regexp.MustCompile(`class="font-semibold text-slate-900"[^>]*href="/performance"`).MatchString(html) {
		t.Errorf("current link not emphasised: %s", html)
	}
	if strings.Contains(renderComponent(t, organisms.MainNav(organisms.NavNone)), "aria-current") {
		t.Errorf("NavNone must mark nothing")
	}
}

// ホーム is a real menu entry now: on `/` it is the current page (issue #896),
// and the logo leads there too.
func TestMainNav_MarksHomeOnTheHomePage(t *testing.T) {
	html := renderComponent(t, organisms.MainNav(organisms.NavHome))
	if !regexp.MustCompile(`<a[^>]*href="/"[^>]*aria-current="page"[^>]*>ホーム</a>`).MatchString(html) {
		t.Errorf("aria-current not on the ホーム link: %s", html)
	}
	if got := strings.Count(html, `aria-current="page"`); got != 1 {
		t.Errorf("aria-current count = %d, want 1", got)
	}
}
