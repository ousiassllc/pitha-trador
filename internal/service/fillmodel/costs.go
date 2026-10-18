package fillmodel

import (
	"context"

	"github.com/ousiassllc/pitha-trador/internal/config/costsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// Costs is the cost assumption of a LONG and a SHORT position: the Model every
// fill of that position (entry and exit alike) is priced with.
type Costs struct {
	Long, Short Model
}

// Uniform is Costs charging m to both directions.
func Uniform(m Model) Costs { return Costs{Long: m, Short: m} }

// For is the Model of direction (domain.JevDirectionShort selects Short; any
// other value is LONG).
func (c Costs) For(direction string) Model {
	if direction == domain.JevDirectionShort {
		return c.Short
	}
	return c.Long
}

// FromSettings builds the Costs the settings describe.
func FromSettings(s costsettings.Settings) Costs {
	model := func(sl costsettings.Slippage) Model {
		return Model{
			SlippageBps:          sl.ContinuousBps,
			OpeningSlippageBps:   sl.OpeningBps,
			ClosingSlippageBps:   sl.ClosingBps,
			FeeBps:               s.FeeBps,
			FeeFixedJPY:          s.FeeFixedJPY,
			ReferenceNotionalJPY: s.ReferenceNotionalJPY,
		}
	}
	c := Costs{Long: model(s.Long), Short: model(s.Short)}
	c.Short.ShortBorrowBps = s.ShortBorrowBps
	return c
}

// Default is the assumption before the operator edits Settings
// (costsettings.Default). Fees are 0: the 証券会社 behind kabuステーションAPI
// (三菱UFJ eスマート証券) charges nothing for 国内株式現物 since 2026-05-18
// (docs/requirements/functional/components-platform.md FR-COST-1). The spread
// is modelled explicitly, so the slippage is only the depth/latency cost
// beyond the touch.
func Default() Costs { return FromSettings(costsettings.Default()) }

// Source supplies the Costs in force at the moment of a fill or a backtest, so
// a Settings change applies without a restart.
type Source interface {
	Costs(ctx context.Context) (Costs, error)
}

// SettingsSource is a Source reading the cost settings from a
// runtime_settings store on every call.
type SettingsSource struct {
	Store costsettings.Store
}

// Costs implements Source.
func (s SettingsSource) Costs(ctx context.Context) (Costs, error) {
	settings, err := costsettings.Load(ctx, s.Store)
	if err != nil {
		return Costs{}, err
	}
	return FromSettings(settings), nil
}
