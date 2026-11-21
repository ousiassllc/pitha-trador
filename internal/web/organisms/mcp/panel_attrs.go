package mcp

import (
	"github.com/a-h/templ"

	"github.com/ousiassllc/pitha-trador/internal/web/atoms"
)

// Attribute helpers of the MCP panel (panel.templ), split out of it to keep the
// template within the linterly per-file limit.

// pollAttrs makes a "starting" panel reload itself until the state settles
// (the swap replaces this element, so the polling ends with it). Other
// states never poll.
//
// A panel that shows the one-time API key never polls: the swap would erase
// the only display of the key.
func pollAttrs(props PanelProps) templ.Attributes {
	if props.State != "starting" || props.RevealedKey != "" {
		return nil
	}
	return templ.Attributes{"hx-get": "/settings/mcp-panel", "hx-trigger": "every 2s", "hx-target": "#mcp-panel", "hx-swap": "innerHTML"}
}

func processAttrs(action, testID string, disabled bool) templ.Attributes {
	attrs := templ.Attributes{
		"hx-post":         "/settings/mcp/" + action,
		"hx-target":       "#mcp-panel",
		"hx-swap":         "innerHTML",
		"hx-disabled-elt": "this",
		"data-testid":     testID,
	}
	if disabled {
		attrs["disabled"] = true
	}
	return attrs
}

func boolAttr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func enabledAttrs(enabled bool) templ.Attributes {
	attrs := templ.Attributes{"data-testid": "mcp-enabled"}
	if enabled {
		attrs["checked"] = true
	}
	return attrs
}

func keyButtonVariant(configured bool) atoms.ButtonVariant {
	if configured {
		return atoms.ButtonDanger
	}
	return atoms.ButtonPrimary
}

// keyButtonAttrs: regenerating asks for confirmation, since it
// invalidates the key the Grok Bot already holds; the first generation has
// nothing to lose.
func keyButtonAttrs(configured bool) templ.Attributes {
	attrs := templ.Attributes{
		"hx-post":         "/settings/mcp/key",
		"hx-target":       "#mcp-panel",
		"hx-swap":         "innerHTML",
		"hx-disabled-elt": "this",
		"data-testid":     "mcp-generate-key",
	}
	if configured {
		attrs["hx-confirm"] = "API キーを再生成します。古いキーは使えなくなります（MCP の再起動後）。よろしいですか？"
	}
	return attrs
}
