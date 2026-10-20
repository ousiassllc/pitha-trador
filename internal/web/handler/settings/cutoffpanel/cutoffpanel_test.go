package cutoffpanel_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/settings/cutoffpanel"
	"github.com/ousiassllc/pitha-trador/internal/web/molecules"
)

func find(t *testing.T, items []molecules.SettingDerivedItem, key string) molecules.SettingDerivedItem {
	t.Helper()
	for _, it := range items {
		if it.Key == key {
			return it
		}
	}
	t.Fatalf("no item %q", key)
	return molecules.SettingDerivedItem{}
}

// Unrecorded provenance is shown muted as 未記録; recorded values are shown as is.
func TestItems_ShowsStatusAndRecordedValues(t *testing.T) {
	s := metalabelsettings.Default()
	items := cutoffpanel.Items(s)
	if got := find(t, items, "cutoff-status").Value; !strings.Contains(got, metalabelsettings.CutoffPlaceholder) {
		t.Errorf("default status = %q, want placeholder", got)
	}
	if it := find(t, items, "cutoff-model"); it.Value != "未記録" || !it.Off {
		t.Errorf("unrecorded model = %+v, want muted 未記録", it)
	}

	s.LLMCutoff, s.LLMCutoffModel, s.LLMCutoffConfirmedOn, s.LLMCutoffSource = "2026-03-31", "jev-2026-03", "2026-10-20", "https://example.com"
	items = cutoffpanel.Items(s)
	if got := find(t, items, "cutoff-status").Value; !strings.Contains(got, metalabelsettings.CutoffConfirmed) {
		t.Errorf("status = %q, want confirmed", got)
	}
	for key, want := range map[string]string{"cutoff": "2026-03-31", "cutoff-model": "jev-2026-03", "cutoff-confirmed-on": "2026-10-20", "cutoff-source": "https://example.com"} {
		if it := find(t, items, key); it.Value != want || it.Off {
			t.Errorf("%s = %+v, want %q", key, it, want)
		}
	}
	find(t, items, "help-1")
}

func TestLoad_ReadsTheFourKeysAndPropagatesAReadError(t *testing.T) {
	values := map[string]string{
		metalabelsettings.KeyLLMCutoff:      "2026-03-31",
		metalabelsettings.KeyLLMCutoffModel: "jev-2026-03",
	}
	items, err := cutoffpanel.Load(func(key string) (string, error) { return values[key], nil })
	if err != nil {
		t.Fatal(err)
	}
	if got := find(t, items, "cutoff-status").Value; !strings.Contains(got, metalabelsettings.CutoffConfirmed) {
		t.Errorf("status = %q, want confirmed", got)
	}
	if _, err := cutoffpanel.Load(func(string) (string, error) { return "", errors.New("boom") }); err == nil {
		t.Error("a read error was swallowed")
	}
}
