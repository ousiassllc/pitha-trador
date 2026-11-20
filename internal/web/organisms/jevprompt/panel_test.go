package jevprompt_test

import (
	"context"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/web/organisms/jevprompt"
)

// issue #1139: "この版をライブに昇格" is an ordinary main button (--accent
// pill), not a --danger one. --danger is kept for Kill and other destructive
// actions.
func TestPanelPromoteButtonIsPrimary(t *testing.T) {
	var sb strings.Builder
	props := jevprompt.PanelProps{ShadowRuns: true, LiveVersion: "v1", CandidateVersion: "v2", Versions: []string{"v1", "v2"}}
	if err := jevprompt.Panel(props).Render(context.Background(), &sb); err != nil {
		t.Fatal(err)
	}
	html := sb.String()
	i := strings.Index(html, `data-testid="jev-prompt-promote"`)
	if i < 0 {
		t.Fatal("promote button not rendered")
	}
	start := strings.LastIndex(html[:i], "<button")
	end := i + strings.Index(html[i:], ">")
	tag := html[start:end]
	for _, want := range []string{"bg-accent text-bone", "rounded-pill"} {
		if !strings.Contains(tag, want) {
			t.Errorf("promote button %q lacks %q", tag, want)
		}
	}
	if strings.Contains(tag, "bg-danger") {
		t.Errorf("promote button %q must not be danger", tag)
	}
}
