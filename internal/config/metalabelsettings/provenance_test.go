package metalabelsettings_test

import (
	"context"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
)

func TestCutoffStatus(t *testing.T) {
	for name, tc := range map[string]struct {
		cutoff, model string
		want          string
		warns         bool
	}{
		"placeholder default":                    {metalabelsettings.DefaultLLMCutoff, "", metalabelsettings.CutoffPlaceholder, true},
		"placeholder even with a recorded model": {metalabelsettings.DefaultLLMCutoff, "jev-x", metalabelsettings.CutoffPlaceholder, true},
		"real date without a model":              {"2026-09-01", "", metalabelsettings.CutoffUnverified, true},
		"real date with a model":                 {"2026-09-01", "jev-x", metalabelsettings.CutoffConfirmed, false},
	} {
		s := metalabelsettings.Default()
		s.LLMCutoff, s.LLMCutoffModel = tc.cutoff, tc.model
		if got := s.CutoffStatus(); got != tc.want {
			t.Errorf("%s: CutoffStatus = %q, want %q", name, got, tc.want)
		}
		if got := s.CutoffWarning() != ""; got != tc.warns {
			t.Errorf("%s: warning present = %v, want %v", name, got, tc.warns)
		}
	}
	if !strings.Contains(metalabelsettings.Default().CutoffWarning(), metalabelsettings.DefaultLLMCutoff) {
		t.Error("the placeholder warning does not name the placeholder date")
	}
}

func TestProvenance_NormalizeApplyAndLoad(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct{ key, raw, want string }{
		{metalabelsettings.KeyLLMCutoffModel, "  jev-2026-09 ", "jev-2026-09"},
		{metalabelsettings.KeyLLMCutoffConfirmedOn, "2026-10-17", "2026-10-17"},
		{metalabelsettings.KeyLLMCutoffSource, "https://example.com/model-card", "https://example.com/model-card"},
	} {
		got, err := metalabelsettings.Normalize(tc.key, tc.raw)
		if err != nil || got != tc.want {
			t.Errorf("Normalize(%s, %q) = %q, %v, want %q", tc.key, tc.raw, got, err, tc.want)
		}
	}
	for _, tc := range []struct{ key, raw string }{
		{metalabelsettings.KeyLLMCutoffModel, ""},
		{metalabelsettings.KeyLLMCutoffModel, "a\nb"},
		{metalabelsettings.KeyLLMCutoffSource, strings.Repeat("あ", metalabelsettings.MaxProvenanceLen+1)},
		{metalabelsettings.KeyLLMCutoffConfirmedOn, "2026-10-1"},
		{metalabelsettings.KeyLLMCutoffConfirmedOn, "soon"},
	} {
		if got, err := metalabelsettings.Normalize(tc.key, tc.raw); err == nil {
			t.Errorf("Normalize(%s, %q) = %q, want an error", tc.key, tc.raw, got)
		}
	}

	s, err := metalabelsettings.Load(ctx, fakeStore{rows: map[string]string{
		metalabelsettings.KeyLLMCutoff:            `"2026-09-01"`,
		metalabelsettings.KeyLLMCutoffModel:       `"jev-x"`,
		metalabelsettings.KeyLLMCutoffConfirmedOn: `"2026-10-17"`,
		metalabelsettings.KeyLLMCutoffSource:      `"vendor docs"`,
	}})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if s.LLMCutoffModel != "jev-x" || s.LLMCutoffConfirmedOn != "2026-10-17" || s.LLMCutoffSource != "vendor docs" || s.CutoffStatus() != metalabelsettings.CutoffConfirmed {
		t.Errorf("Load did not carry the provenance: %+v", s)
	}
	if text, ok := s.Text(metalabelsettings.KeyLLMCutoffSource); !ok || text != "vendor docs" {
		t.Errorf("Text(source) = %q, %v", text, ok)
	}
	if text, ok := metalabelsettings.Default().Text(metalabelsettings.KeyLLMCutoffModel); !ok || text != "" {
		t.Errorf("default Text(model) = %q, %v, want empty", text, ok)
	}
}

// Issue #809: replacing the placeholder date and then recording the Jev model
// takes the status placeholder -> unverified -> confirmed, and the warning is
// gone only at confirmed. A date alone never reaches confirmed.
func TestCutoffStatus_PlaceholderReplacedThenConfirmed(t *testing.T) {
	s := metalabelsettings.Default()
	if s.CutoffStatus() != metalabelsettings.CutoffPlaceholder || s.CutoffWarning() == "" {
		t.Fatalf("default: status %q, warning %q", s.CutoffStatus(), s.CutoffWarning())
	}
	s.LLMCutoff = "2026-03-31"
	s.LLMCutoffConfirmedOn, s.LLMCutoffSource = "2026-10-20", "https://example.com/model-card"
	if s.CutoffStatus() != metalabelsettings.CutoffUnverified || s.CutoffWarning() == "" {
		t.Fatalf("date and source without a model: status %q, warning %q", s.CutoffStatus(), s.CutoffWarning())
	}
	s.LLMCutoffModel = "jev-2026-03"
	if s.CutoffStatus() != metalabelsettings.CutoffConfirmed || s.CutoffWarning() != "" {
		t.Fatalf("with the model: status %q, warning %q", s.CutoffStatus(), s.CutoffWarning())
	}
}

func TestCutoffStatusLabelAndHelp(t *testing.T) {
	for _, status := range []string{metalabelsettings.CutoffPlaceholder, metalabelsettings.CutoffUnverified, metalabelsettings.CutoffConfirmed} {
		label, effect := metalabelsettings.CutoffStatusLabel(status)
		if label == "" || effect == "" {
			t.Errorf("CutoffStatusLabel(%q) = %q, %q: want a label and a non-empty effect", status, label, effect)
		}
	}
	help := strings.Join(metalabelsettings.CutoffHelp(), "\n")
	// The screen names the four items in plain words, never by setting key (issue #904).
	for _, name := range []string{"学習データの終わり", "モデル名", "確認日", "出典"} {
		if !strings.Contains(help, name+":") {
			t.Errorf("the help does not say what goes in %s", name)
		}
	}
	for _, key := range []string{metalabelsettings.KeyLLMCutoff, metalabelsettings.KeyLLMCutoffModel, metalabelsettings.KeyLLMCutoffConfirmedOn, metalabelsettings.KeyLLMCutoffSource} {
		if strings.Contains(help, key) {
			t.Errorf("the help shows the setting key %s", key)
		}
		if !metalabelsettings.IsCutoffKey(key) {
			t.Errorf("IsCutoffKey(%s) = false", key)
		}
	}
	if metalabelsettings.IsCutoffKey(metalabelsettings.KeyMinEVBps) {
		t.Error("IsCutoffKey accepts an unrelated key")
	}
}

// This build is Paper-only (issue #815): the single run-mode definition must
// stay Paper until a Live build is added deliberately.
func TestRuntimeModeIsPaper(t *testing.T) {
	if metalabelsettings.RuntimeMode != metalabelsettings.ModePaper {
		t.Fatalf("RuntimeMode = %q, want %q", metalabelsettings.RuntimeMode, metalabelsettings.ModePaper)
	}
}
