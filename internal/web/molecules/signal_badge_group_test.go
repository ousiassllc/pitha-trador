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

// issue #1139 (T5): the confidence is a number to compare, so it is Mono with
// tabular figures like the scanner table; the entry-quality word is not.
func TestSignalBadgeGroup_ConfidenceIsMonoAndEntryQualityIsNot(t *testing.T) {
	dir, conf, quality := "long", 0.72, "良好"
	html := render(t, molecules.SignalBadgeGroup(&dir, &conf, &quality))
	if !strings.Contains(html, `<span class="text-note font-mono tabular-nums text-mute">`) {
		t.Errorf("confidence must be font-mono tabular-nums: %s", html)
	}
	if !strings.Contains(html, `<span class="text-note text-ink">良好</span>`) {
		t.Errorf("entry quality must stay plain text: %s", html)
	}
}
