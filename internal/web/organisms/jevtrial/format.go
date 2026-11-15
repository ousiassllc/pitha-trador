package jevtrial

import "strconv"

func itoa(n int) string { return strconv.Itoa(n) }

// armName is the form field name of a row's field (`arm-<n>-<field>`); the
// handler reads exactly these names.
func armName(i int, field string) string { return "arm-" + strconv.Itoa(i) + "-" + field }

// armDOMID is the element id of a row's field, for its label.
func armDOMID(i int, field string) string { return "jev-trial-arm-" + strconv.Itoa(i) + "-" + field }

// versionChoices is the registered prompt versions, plus the row's own value
// when it is not among them (so a stored arm keeps showing its version; saving
// it is then refused with the reason).
func versionChoices(versions []string, current string) []string {
	if current == "" {
		return versions
	}
	for _, v := range versions {
		if v == current {
			return versions
		}
	}
	return append(append([]string(nil), versions...), current)
}
