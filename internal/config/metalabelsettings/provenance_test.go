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
