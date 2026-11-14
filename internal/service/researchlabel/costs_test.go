package researchlabel

import (
	"context"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/barriersettings"
	"github.com/ousiassllc/pitha-trador/internal/config/costsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
)

type mapStore map[string]string

func (m mapStore) Get(_ context.Context, key string) (string, bool, error) {
	v, ok := m[key]
	return v, ok, nil
}

func TestCostsFromSettings_AreTheCostsTheLiveLabelerAssumesForEachSide(t *testing.T) {
	// A non-default Settings state: barrier cost 14, fee 1.5 bps, slippage 3 bps
	// LONG / 4 bps SHORT (default 2), borrow 6 bps.
	store := mapStore{
		barriersettings.KeyCostBps:              "14",
		costsettings.KeyFeeBps:                  "1.5",
		costsettings.KeySlippageLongContinuous:  "3",
		costsettings.KeySlippageShortContinuous: "4",
		costsettings.KeyShortBorrowBps:          "6",
	}
	got, err := CostsFromSettings(context.Background(), store)
	if err != nil {
		t.Fatal(err)
	}
	barriers, err := barriersettings.Load(context.Background(), store)
	if err != nil {
		t.Fatal(err)
	}
	for direction, have := range map[string]float64{domain.JevDirectionLong: got.Long, domain.JevDirectionShort: got.Short} {
		// The live labeler's call (calibration.Labeler.Compute).
		live, err := costsettings.LabelBarriers(context.Background(), store, barriers, direction)
		if err != nil {
			t.Fatal(err)
		}
		if have != live.CostBps {
			t.Errorf("%s cost = %v, live label cost = %v", direction, have, live.CostBps)
		}
	}
	// By hand: 14 + 2*1.5 + 2*(3-2) = 19; SHORT 14 + 3 + 2*(4-2) + 6 = 27.
	near(t, "long cost", got.Long, 19)
	near(t, "short cost", got.Short, 27)
	// And they differ from the shipped defaults, so a hard-coded 10 would fail.
	def, err := CostsFromSettings(context.Background(), mapStore{})
	if err != nil {
		t.Fatal(err)
	}
	near(t, "default long cost", def.Long, 10)
	near(t, "default short cost", def.Short, 10)
}
