package molecules_test

import (
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/web/molecules"
)

// The theme switch (issue #978) offers follow-the-OS, light and dark as
// buttons the theme-toggle script reads by `data-theme-choice`; 自動 starts
// pressed, and the control is hidden when printing.
func TestThemeToggle_OffersAutoLightDarkWithAutoPressed(t *testing.T) {
	html := render(t, molecules.ThemeToggle())
	for _, want := range []string{
		`role="group"`,
		`aria-label="テーマ"`,
		`print:hidden`,
		`aria-pressed="true" data-theme-choice="auto"`,
		`aria-pressed="false" data-theme-choice="light"`,
		`aria-pressed="false" data-theme-choice="dark"`,
		`>自動<`,
		`>ライト<`,
		`>ダーク<`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("ThemeToggle lacks %q: %s", want, html)
		}
	}
	if got := strings.Count(html, "<button"); got != 3 {
		t.Errorf("ThemeToggle renders %d buttons, want 3", got)
	}
	// issue #1018 (C6): text-mute on the bg-accent-soft track is under AA 4.5:1.
	if strings.Contains(html, "text-mute") || strings.Count(html, "text-ink") != 3 {
		t.Errorf("ThemeToggle labels must be text-ink, not text-mute: %s", html)
	}
	// WCAG 1.4.1: the selected segment is told apart by weight, not by the
	// --surface face alone (SemiBold is allowed on buttons, brand-guide T2).
	if got := strings.Count(html, "aria-pressed:font-semibold"); got != 3 {
		t.Errorf("every ThemeToggle segment must be semibold while pressed (got %d): %s", got, html)
	}
}
