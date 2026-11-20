// Package costpanel builds the cost consistency panel of the Settings
// "売買コストの想定" and "トリプルバリア" groups (issue #779): the label cost
// (barrier base cost moved by the fill model's cost) beside the fill model's
// round-trip cost, with a warning when they have drifted apart. It is the
// Settings-screen counterpart of the Calibration screen's cost_mismatch
// (exitalign.CostWarnings, one definition for both) and changes no setting.
package costpanel

import (
	"context"
	"fmt"
	"log/slog"
	"math"

	"github.com/ousiassllc/pitha-trador/internal/config/barriersettings"
	"github.com/ousiassllc/pitha-trador/internal/config/costsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/exitalign"
	"github.com/ousiassllc/pitha-trador/internal/service/opsettings"
	"github.com/ousiassllc/pitha-trador/internal/web/molecules"
)

// Title is the panel's title.
const Title = "ラベルと約定モデルのコスト整合"

// Getter reads the effective value of one runtime setting (the Settings screen's
// OperationalSettings satisfies it).
type Getter interface {
	Get(ctx context.Context, key string) (opsettings.Value, error)
}

// Derived is the panel as the Settings groups show it: Load over ops. A read
// failure is logged and shown as such instead of failing the screen.
func Derived(ctx context.Context, ops Getter) []molecules.SettingDerivedItem {
	items, err := Load(func(key string) (string, error) {
		v, err := ops.Get(ctx, key)
		return v.Current, err
	})
	if err != nil {
		slog.Error("settings: read the cost settings for the consistency panel; showing it as unreadable", "error", err)
		return []molecules.SettingDerivedItem{{Key: "error", Label: "コスト整合を読み取れませんでした", Off: true}}
	}
	return items
}

// Load reads the effective barrier and cost settings with get (the effective
// value of one runtime_settings key, as text) and renders them with Items. The
// first read or parse error is returned.
func Load(get func(key string) (string, error)) ([]molecules.SettingDerivedItem, error) {
	b, s := barriersettings.Default(), costsettings.Default()
	for _, key := range barriersettings.Keys() {
		raw, err := get(key)
		if err != nil {
			return nil, err
		}
		if err := barriersettings.Apply(&b, key, raw); err != nil {
			return nil, err
		}
	}
	for _, key := range costsettings.Keys() {
		raw, err := get(key)
		if err != nil {
			return nil, err
		}
		if err := costsettings.Apply(&s, key, raw); err != nil {
			return nil, err
		}
	}
	return Items(b, s), nil
}

// Items renders the label cost and the fill model's round-trip cost of each
// direction and, when exitalign.CostWarnings finds a mismatch, one warning line
// per finding (otherwise one line saying they agree).
func Items(b barriersettings.Barriers, s costsettings.Settings) []molecules.SettingDerivedItem {
	items := []molecules.SettingDerivedItem{
		cost("long", "LONG", b, s, domain.JevDirectionLong),
		cost("short", "SHORT", b, s, domain.JevDirectionShort),
	}
	warnings := exitalign.CostWarnings(b, s)
	for _, w := range warnings {
		items = append(items, molecules.SettingDerivedItem{Key: "warn-" + w.Code, Warn: true, Label: w.Message})
	}
	if len(warnings) == 0 {
		items = append(items, molecules.SettingDerivedItem{Key: "aligned", Label: "整合", Value: "ラベルのコストと約定モデルのコストは整合しています"})
	}
	return items
}

// cost is one direction's line: the label cost, the fill model's round-trip
// cost and the difference (the allowance for the spread, which the fills take
// from the board).
func cost(key, name string, b barriersettings.Barriers, s costsettings.Settings, direction string) molecules.SettingDerivedItem {
	label, fill := s.LabelCostBps(b.CostBps, direction), s.RoundTripBps(direction)
	return molecules.SettingDerivedItem{
		Key: key, Label: name + " の往復コスト",
		Value:  fmt.Sprintf("ラベル %s bps ／ 約定モデル %s bps", bps(label), bps(fill)),
		Detail: fmt.Sprintf("差 %s bps（スプレッド分の見込み。許容 0〜%g bps）", bps(label-fill), exitalign.MaxCostAllowanceBps),
	}
}

func bps(v float64) string { return fmt.Sprintf("%g", math.Round(v*100)/100) }
