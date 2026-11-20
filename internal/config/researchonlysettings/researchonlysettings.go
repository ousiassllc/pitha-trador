// Package researchonlysettings joins the settings of the research-only features of issues
// #1089 (daily evaluation record), #1092 (research model) and #1091 (promotion checklist
// criteria) so that opsettings dispatches them as one family. None of them is a trading
// input; the switches default to off, and the criteria can only be made stricter than their
// documented floor (promotionsettings.Validate).
package researchonlysettings

import (
	"github.com/ousiassllc/pitha-trador/internal/config/promotionsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/researchjobsettings"
)

// Keys are the keys of researchjobsettings then promotionsettings.
func Keys() []string {
	return append(researchjobsettings.Keys(), promotionsettings.Keys()...)
}

// IsKey reports whether key belongs to either family.
func IsKey(key string) bool {
	return researchjobsettings.IsKey(key) || promotionsettings.IsKey(key)
}

// IsIntegerKey: the switches, the retentions and the two whole-number criteria.
func IsIntegerKey(key string) bool {
	return researchjobsettings.IsKey(key) || promotionsettings.IsIntegerKey(key)
}

// DefaultText is the default of key, encoded as it is stored.
func DefaultText(key string) string {
	if promotionsettings.IsKey(key) {
		return promotionsettings.DefaultText(key)
	}
	return researchjobsettings.DefaultText(key)
}

// Validate checks one encoded value; each value stands alone.
func Validate(key, encoded string) error {
	if promotionsettings.IsKey(key) {
		return promotionsettings.Validate(key, encoded)
	}
	return researchjobsettings.Validate(key, encoded)
}
