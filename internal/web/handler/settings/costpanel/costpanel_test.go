package costpanel_test

import (
	"errors"
	"strconv"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/barriersettings"
	"github.com/ousiassllc/pitha-trador/internal/config/costsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/exitalign"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/settings/costpanel"
	"github.com/ousiassllc/pitha-trador/internal/web/molecules"
)

func find(items []molecules.SettingDerivedItem, key string) (molecules.SettingDerivedItem, bool) {
	for _, it := range items {
		if it.Key == key {
			return it, true
		}
	}
	return molecules.SettingDerivedItem{}, false
}

// At the shipped defaults the label (10 bps) and the fill model (4 bps, spread
// excluded) agree: no warning, one line saying so.
func TestItems_DefaultsAreAligned(t *testing.T) {
	items := costpanel.Items(barriersettings.Default(), costsettings.Default())
	if _, ok := find(items, "aligned"); !ok {
		t.Errorf("defaults do not report alignment: %+v", items)
	}
	for _, it := range items {
		if it.Warn {
			t.Errorf("defaults warn: %+v", it)
		}
	}
	long, _ := find(items, "long")
	if long.Value != "ラベル 10 bps ／ 約定モデル 4 bps" {
		t.Errorf("LONG line = %q", long.Value)
	}
}

// A base cost far from the default warns (cost_mismatch), like the Calibration
// screen; raising the fee or slippage alone does not.
func TestItems_BaseCostStrayWarnsButFeeDoesNot(t *testing.T) {
	b := barriersettings.Default()
	b.CostBps = 40
	items := costpanel.Items(b, costsettings.Default())
	if it, ok := find(items, "warn-"+exitalign.WarnCost); !ok || !it.Warn {
		t.Errorf("base cost 40 does not warn: %+v", items)
	}
	if _, ok := find(items, "aligned"); ok {
		t.Errorf("a warning and the aligned line together: %+v", items)
	}

	s := costsettings.Default()
	s.FeeBps = 10
	s.Long.ContinuousBps, s.Short.ContinuousBps = 12, 12
	items = costpanel.Items(barriersettings.Default(), s)
	if _, ok := find(items, "aligned"); !ok {
		t.Errorf("raising the fee and slippage on the default base cost warns: %+v", items)
	}
}

// A SHORT 貸株料 moves only the SHORT label cost: the directions drift apart.
func TestItems_ShortBorrowWarnsAsymmetry(t *testing.T) {
	s := costsettings.Default()
	s.ShortBorrowBps = 8
	items := costpanel.Items(barriersettings.Default(), s)
	if it, ok := find(items, "warn-"+exitalign.WarnCostSides); !ok || !it.Warn {
		t.Errorf("a 8 bps borrow fee does not warn: %+v", items)
	}
}

func TestLoad_ReadsEffectiveValuesAndPropagatesErrors(t *testing.T) {
	values := map[string]string{barriersettings.KeyCostBps: "40"}
	get := func(key string) (string, error) {
		if v, ok := values[key]; ok {
			return v, nil
		}
		if text, ok := barriersettings.Default().Text(key); ok {
			return text, nil
		}
		text, _ := costsettings.Default().Text(key)
		return text, nil
	}
	items, err := costpanel.Load(get)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, ok := find(items, "warn-"+exitalign.WarnCost); !ok {
		t.Errorf("stored base cost 40 not read: %+v", items)
	}
	// Every default round-trips through the text form Settings reads back.
	for _, key := range costsettings.Keys() {
		text, _ := costsettings.Default().Text(key)
		if _, err := strconv.ParseFloat(text, 64); err != nil {
			t.Errorf("default %s text %q is not a number", key, text)
		}
	}

	boom := errors.New("boom")
	if _, err := costpanel.Load(func(string) (string, error) { return "", boom }); !errors.Is(err, boom) {
		t.Errorf("Load error = %v, want boom", err)
	}
	if _, err := costpanel.Load(func(string) (string, error) { return "not-a-number", nil }); err == nil {
		t.Error("Load accepted an unparsable value")
	}
}
