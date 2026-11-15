package opsettings

import (
	"github.com/ousiassllc/pitha-trador/internal/config/promotionsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/researchjobsettings"
)

// The settings of the research-only features of issues #1089 (daily evaluation record),
// #1092 (research model) and #1091 (promotion checklist criteria). None of them is a
// trading input; the two switches default to off, and the criteria can only be made
// stricter than their documented floor (promotionsettings.Validate).

// researchOnlyKeys are the keys of researchjobsettings then promotionsettings.
func researchOnlyKeys() []string {
	return append(researchjobsettings.Keys(), promotionsettings.Keys()...)
}

func researchOnlyKey(key string) bool {
	return researchjobsettings.IsKey(key) || promotionsettings.IsKey(key)
}

// researchOnlyIntegerKey: the switches, the retentions and the two whole-number criteria.
func researchOnlyIntegerKey(key string) bool {
	return researchjobsettings.IsKey(key) || promotionsettings.IsIntegerKey(key)
}

func researchOnlyDefaultText(key string) string {
	if promotionsettings.IsKey(key) {
		return promotionsettings.DefaultText(key)
	}
	return researchjobsettings.DefaultText(key)
}

// validateResearchOnly checks one encoded value; each value stands alone.
func validateResearchOnly(key, encoded string) error {
	var err error
	if promotionsettings.IsKey(key) {
		err = promotionsettings.Validate(key, encoded)
	} else {
		err = researchjobsettings.Validate(key, encoded)
	}
	if err != nil {
		return &InvalidValueError{Reason: err.Error()}
	}
	return nil
}
