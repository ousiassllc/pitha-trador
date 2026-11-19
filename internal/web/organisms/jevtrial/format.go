package jevtrial

import (
	"strconv"

	"github.com/ousiassllc/pitha-trador/internal/config/jevtrialsettings"
)

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

// providerChoice is one option of an arm's provider select.
type providerChoice struct{ Value, Label string }

// providerOrDefault is the provider a row shows: a new row has none yet and
// starts on OpenRouter.
func providerOrDefault(provider string) string {
	if provider == "" {
		return jevtrialsettings.ProviderOpenRouter
	}
	return provider
}

// providerChoices are the providers an arm can use. A value the form does not
// know (it cannot be saved) is still listed, so the row shows what it holds.
func providerChoices(current string) []providerChoice {
	choices := []providerChoice{
		{jevtrialsettings.ProviderOpenRouter, "OpenRouter"},
		{jevtrialsettings.ProviderCustom, "独自 API（custom）"},
		{jevtrialsettings.ProviderCodexChatGPT, "ChatGPT サブスク（codex_chatgpt）"},
	}
	for _, c := range choices {
		if c.Value == current || current == "" {
			return choices
		}
	}
	return append(choices, providerChoice{current, current})
}
