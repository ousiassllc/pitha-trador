// Package cutoffpanel builds the LLM cutoff status panel of the Settings
// "メタラベリング" group (issue #809): the status (placeholder / unverified /
// confirmed) with the recorded provenance, and the short how-to of confirming
// the official cutoff date.
package cutoffpanel

import (
	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/web/molecules"
)

// Load reads the four cutoff settings' effective values with get and renders
// them with Items. The first read error is returned.
func Load(get func(key string) (string, error)) ([]molecules.SettingDerivedItem, error) {
	var s metalabelsettings.Settings
	for key, dst := range map[string]*string{
		metalabelsettings.KeyLLMCutoff:            &s.LLMCutoff,
		metalabelsettings.KeyLLMCutoffModel:       &s.LLMCutoffModel,
		metalabelsettings.KeyLLMCutoffConfirmedOn: &s.LLMCutoffConfirmedOn,
		metalabelsettings.KeyLLMCutoffSource:      &s.LLMCutoffSource,
	} {
		v, err := get(key)
		if err != nil {
			return nil, err
		}
		*dst = v
	}
	return Items(s), nil
}

// Items renders s's cutoff status as the panel's lines. The status is
// s.CutoffStatus, the same classification the nightly adoption and the MCP's
// settings_get use.
func Items(s metalabelsettings.Settings) []molecules.SettingDerivedItem {
	label, effect := metalabelsettings.CutoffStatusLabel(s.CutoffStatus())
	items := []molecules.SettingDerivedItem{
		{Key: "cutoff-status", Label: "状態", Value: label, Detail: effect},
		recorded("cutoff", "学習データの終わり", s.LLMCutoff),
		recorded("cutoff-model", "確認したモデル", s.LLMCutoffModel),
		recorded("cutoff-confirmed-on", "確認日", s.LLMCutoffConfirmedOn),
		recorded("cutoff-source", "出典", s.LLMCutoffSource),
		{Key: "help-heading", Heading: true, Label: "公式の日付の確認手順", Help: metalabelsettings.CutoffHelp()},
	}
	return items
}

// recorded is one recorded value; an empty value is shown muted as 未記録.
func recorded(key, label, value string) molecules.SettingDerivedItem {
	if value == "" {
		return molecules.SettingDerivedItem{Key: key, Label: label, Value: "未記録", Off: true}
	}
	return molecules.SettingDerivedItem{Key: key, Label: label, Value: value}
}
