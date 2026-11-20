// Package researchrecordsettings is the one view of the two research record
// settings sets that are stored as a switch plus a symbol list in
// runtime_settings (issue #1084): the FX candle record (researchfxsettings,
// research.fx.*) and the index value record (researchindexsettings,
// research.index.*). opsettings (the Settings screen's storage) reaches both only
// through here, so adding a record does not grow its dispatch code.
package researchrecordsettings

import (
	"slices"

	"github.com/ousiassllc/pitha-trador/internal/config/researchfxsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/researchindexsettings"
)

// Keys returns every runtime_settings key of the records (a new slice).
func Keys() []string {
	return slices.Concat(researchfxsettings.Keys(), researchindexsettings.Keys())
}

// IsKey reports whether key is one of Keys.
func IsKey(key string) bool { return researchfxsettings.IsKey(key) || researchindexsettings.IsKey(key) }

// IsSwitchKey reports whether key is a record's on/off switch (JSON integer 1 / 0).
func IsSwitchKey(key string) bool {
	return key == researchfxsettings.KeyEnabled || key == researchindexsettings.KeyEnabled
}

// IsSymbolsKey reports whether key is a record's symbol list (a JSON string of
// comma-separated names).
func IsSymbolsKey(key string) bool {
	return key == researchfxsettings.KeySymbols || key == researchindexsettings.KeySymbols
}

// DefaultText is the stored-form text of key while nothing is stored.
func DefaultText(key string) string {
	if researchindexsettings.IsKey(key) {
		return researchindexsettings.DefaultText(key)
	}
	return researchfxsettings.DefaultText(key)
}

// Normalize checks a symbol list of IsSymbolsKey key and returns it canonical.
func Normalize(key, raw string) (string, error) {
	if key == researchindexsettings.KeySymbols {
		return researchindexsettings.Normalize(raw)
	}
	return researchfxsettings.Normalize(raw)
}

// Validate checks one stored value of key (a switch or a symbol list).
func Validate(key, jsonValue string) error {
	if researchindexsettings.IsKey(key) {
		return researchindexsettings.Validate(key, jsonValue)
	}
	return researchfxsettings.Validate(key, jsonValue)
}
