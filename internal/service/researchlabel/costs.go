package researchlabel

import (
	"context"
	"fmt"

	"github.com/ousiassllc/pitha-trador/internal/config/barriersettings"
	"github.com/ousiassllc/pitha-trador/internal/config/costsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// SettingsStore is the runtime_settings read access the label costs need;
// *system.RuntimeSettingsRepository implements it.
type SettingsStore interface {
	barriersettings.Store
	costsettings.Store
}

// CostsFromSettings is the cost the existing label assumes for each side now:
// the barrier cost (calibration.barrier_cost_bps) moved by the cost.* settings
// through costsettings.LabelBarriers, the same function the live labeler
// (calibration.Labeler.Compute) calls. The research label thereby never
// assumes a different cost than the label it is compared with.
func CostsFromSettings(ctx context.Context, store SettingsStore) (Costs, error) {
	barriers, err := barriersettings.Load(ctx, store)
	if err != nil {
		return Costs{}, fmt.Errorf("researchlabel: load barrier settings: %w", err)
	}
	var costs Costs
	for direction, dst := range map[string]*float64{domain.JevDirectionLong: &costs.Long, domain.JevDirectionShort: &costs.Short} {
		moved, err := costsettings.LabelBarriers(ctx, store, barriers, direction)
		if err != nil {
			return Costs{}, fmt.Errorf("researchlabel: load cost settings: %w", err)
		}
		*dst = moved.CostBps
	}
	return costs, nil
}
