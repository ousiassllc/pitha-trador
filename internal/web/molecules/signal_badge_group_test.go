package molecules_test

import (
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/web/molecules"
)

// issue #1022 (C6): tone-mute carries the pill's text color (--ink); a
// text-mute on the same element would override it with a color under AA 4.5:1.
func TestSignalBadgeGroup_PendingPillDoesNotSetTextMute(t *testing.T) {
	html := render(t, molecules.SignalBadgeGroup(nil, nil, nil))
	if !strings.Contains(html, "tone-mute") || strings.Contains(html, "text-mute") {
		t.Errorf("pending pill must be tone-mute without text-mute: %s", html)
	}
}
