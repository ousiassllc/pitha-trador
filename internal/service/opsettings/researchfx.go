package opsettings

import (
	"github.com/ousiassllc/pitha-trador/internal/config/researchfxsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/watchsettings"
)

// normalizeSymbols normalizes a symbol-list setting: the research FX pairs
// (research.fx.symbols, GMO pair names) or the observation-only stock codes.
func normalizeSymbols(key, raw string) (string, error) {
	if key == researchfxsettings.KeySymbols {
		return researchfxsettings.Normalize(raw)
	}
	return watchsettings.NormalizeSymbols(raw)
}

// validateResearchFX checks one stored research FX value (the switch, or the
// pairs); each value stands alone.
func validateResearchFX(key, encoded string) error {
	var err error
	if key == researchfxsettings.KeyEnabled {
		_, err = researchfxsettings.ParseEnabled(encoded)
	} else {
		_, err = researchfxsettings.ParseSymbols(encoded)
	}
	if err != nil {
		return &InvalidValueError{Reason: err.Error()}
	}
	return nil
}
