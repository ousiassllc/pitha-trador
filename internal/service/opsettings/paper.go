package opsettings

import (
	"context"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/config/papersettings"
)

// paperLimitsBase is the Paper limits that apply to the papersettings keys
// while nothing is stored: config/risk.yaml's (WithRiskReference) or, without
// one, the issue #804 Paper initial values.
func (s *Service) paperLimitsBase() config.RiskLimits {
	if s.paperBase != nil {
		return *s.paperBase
	}
	return config.RiskLimits{
		InitialCapital:          papersettings.DefaultInitialCapitalJPY,
		MaxPositionPerSymbolPct: papersettings.DefaultMaxPositionPerSymbolPct,
		MaxTotalExposurePct:     papersettings.DefaultMaxTotalExposurePct,
	}
}

// paperDefaultText is the value of a papersettings key while nothing is
// stored: the risk.yaml fallback.
func (s *Service) paperDefaultText(key string) string {
	base := s.paperLimitsBase()
	switch key {
	case papersettings.KeyInitialCapitalJPY:
		return formatNumber(base.InitialCapital)
	case papersettings.KeyMaxPositionPerSymbolPct:
		return formatNumber(base.MaxPositionPerSymbolPct)
	default:
		return formatNumber(base.MaxTotalExposurePct)
	}
}

// PaperLimits returns the Paper Risk Engine's limits in force now: the
// Settings values (papersettings) over config/risk.yaml's. It is the same
// read the Risk Engine does on every evaluation.
func (s *Service) PaperLimits(ctx context.Context) (config.RiskLimits, error) {
	return papersettings.Effective(ctx, s.store, s.paperLimitsBase())
}
