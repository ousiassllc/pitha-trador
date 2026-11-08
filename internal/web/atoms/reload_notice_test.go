package atoms_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/web/atoms"
)

// PR #1044 M5: the persistent notice sits beside the stale-session toast, whose
// role="alert" is the announcement. Any role / aria-live on the notice would make
// a screen reader read the same fact twice, and a close button would let the only
// recovery action be dismissed instead of resolved.
func TestReloadNotice_IsSilentPersistentAndKeyboardOperable(t *testing.T) {
	body := renderWithLabel(t, atoms.ReloadNotice(), "")

	for _, banned := range []string{"role=", "aria-live", "data-toast", "data-toast-dismiss", "tabindex"} {
		if strings.Contains(body, banned) {
			t.Errorf("notice must not carry %q: %s", banned, body)
		}
	}
	// A native <button type="button"> is reachable by Tab and runs on Enter/Space.
	button := regexp.MustCompile(`<button\b[^>]*\bdata-reload-notice-reload\b[^>]*>`).FindString(body)
	if button == "" || !strings.Contains(button, `type="button"`) || regexp.MustCompile(`\s(hidden|disabled)[\s>=]`).MatchString(button) {
		t.Errorf("want one visible enabled type=button reload action, got %q in %s", button, body)
	}
	if !strings.Contains(body, "再読み込み") {
		t.Errorf("reload action lacks its label: %s", body)
	}
	for _, want := range []string{"data-reload-notice ", "data-reload-notice-message", "print:hidden", "tone-warning"} {
		if !strings.Contains(body, want) {
			t.Errorf("notice lacks %q: %s", want, body)
		}
	}
}
