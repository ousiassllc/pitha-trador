package organismstest

import (
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/web/organisms"
)

// issue #753: the header banner names the rule that stopped new entries.
func TestDailyHaltBanner_ShowsRuleFiguresAndRecovery(t *testing.T) {
	html := renderComponent(t, organisms.DailyHaltBanner(organisms.DailyHaltBannerProps{
		Reason: "daily_loss_limit_jpy", Label: "1日の損失上限", Message: "当日損益 -50000円 が損失上限 -50000円 に達しました。",
	}))
	for _, want := range []string{
		`data-testid="daily-halt-banner"`, `data-reason="daily_loss_limit_jpy"`,
		"本日の新規エントリーを停止しました", "1日の損失上限", "-50000円", "決済は通常どおり", "翌営業日に自動で解除",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("banner missing %q in %s", want, html)
		}
	}
	if strings.Contains(html, "role=") {
		t.Errorf("banner must not carry its own role (issue #626): %s", html)
	}
}

func TestDailyHaltBanner_RendersNothingWithoutAHalt(t *testing.T) {
	html := renderComponent(t, organisms.DailyHaltBanner(organisms.DailyHaltBannerProps{}))
	if strings.Contains(html, "daily-halt-banner") {
		t.Errorf("rendered a banner without a halt: %s", html)
	}
}
