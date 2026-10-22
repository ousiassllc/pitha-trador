package organismstest

import (
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/web/organisms"
)

// issue #802: each active alert is named in fixed wording with its start time.
func TestHealthAlertBanner_NamesEachAlertWithItsStartAndSaysItClearsItself(t *testing.T) {
	html := renderComponent(t, organisms.HealthAlertBanner([]organisms.HealthAlertView{
		{Code: "kabu_not_logged_in", Since: "08:45"},
		{Code: "market_data_idle", Since: "10:05"},
	}))
	for _, want := range []string{
		`data-testid="health-alert-banner"`, `data-code="kabu_not_logged_in"`, `data-code="market_data_idle"`,
		"kabuステーションに未ログイン", "08:45から続いています", "市況データの取得が止まっています", "10:05から続いています",
		"自動的に消えます",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("banner missing %q in %s", want, html)
		}
	}
	if strings.Contains(html, "role=") {
		t.Errorf("banner must not carry its own role (issue #626): %s", html)
	}
}

// issue #863: not logged in and not running read differently - the first says
// to log in, the second to start kabuステーション (and check the API setting).
func TestHealthAlertBanner_KabuNotLoggedInAndUnreachableTellWhatToDo(t *testing.T) {
	login := renderComponent(t, organisms.HealthAlertBanner([]organisms.HealthAlertView{{Code: "kabu_not_logged_in", Since: "08:45"}}))
	for _, want := range []string{`data-code="kabu_not_logged_in"`, "未ログインです", "kabuステーションにログインしてください"} {
		if !strings.Contains(login, want) {
			t.Errorf("not-logged-in banner missing %q in %s", want, login)
		}
	}
	if strings.Contains(login, "起動してください") {
		t.Errorf("not-logged-in banner tells the operator to start kabuステーション: %s", login)
	}
	down := renderComponent(t, organisms.HealthAlertBanner([]organisms.HealthAlertView{{Code: "kabu_unreachable", Since: "08:45"}}))
	for _, want := range []string{`data-code="kabu_unreachable"`, "接続できません", "未起動", "08:45から続いています", "kabuステーションを起動してログインしてください", "自動的に消えます"} {
		if !strings.Contains(down, want) {
			t.Errorf("unreachable banner missing %q in %s", want, down)
		}
	}
}

func TestHealthAlertBanner_RendersNothingWithoutAlerts(t *testing.T) {
	if html := renderComponent(t, organisms.HealthAlertBanner(nil)); strings.Contains(html, "health-alert-banner") {
		t.Errorf("rendered a banner without alerts: %s", html)
	}
}
