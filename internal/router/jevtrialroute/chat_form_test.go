package jevtrialroute_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/jevtrialsettings"
)

func chatRow(n int, mod func(url.Values, string)) url.Values {
	row := armRow(n, "or-chat", "openai/gpt-6.2-astra")
	p := "arm-" + string(rune('0'+n)) + "-"
	row.Set(p+"endpoint", jevtrialsettings.EndpointChatCompletions)
	row.Set(p+"list_price_usd_per_mtok", "8")
	row.Set(p+"list_price_output_usd_per_mtok", "40")
	if mod != nil {
		mod(row, p)
	}
	return row
}

// The Settings form offers the chat endpoint as a third choice and an output price field;
// a chat arm saves, shows as selected, and keeps both prices (issue #1224).
func TestChatEndpointArmSavesAndShowsOnTheForm(t *testing.T) {
	a := newApp(t)
	panel := a.get("/settings/jev-trial-panel")
	_ = panel
	rec := a.post("/settings/jev-trial/arms/add", url.Values{}, true)
	if body := rec.Body.String(); !strings.Contains(body, `value="/v1/chat/completions"`) || !strings.Contains(body, `jev-trial-arm-list-price-output`) {
		t.Fatalf("the editor lacks the chat option or the output price field: %s", body)
	}
	if rec := a.post("/settings/jev-trial", join(url.Values{"enabled": {"on"}}, chatRow(0, nil)), true); rec.Code != http.StatusOK {
		t.Fatalf("save = %d %s", rec.Code, rec.Body.String())
	}
	raw, _ := a.stored(jevtrialsettings.KeyArms)
	arms, err := jevtrialsettings.ParseArms(raw)
	if err != nil || len(arms) != 1 || !arms[0].IsChat() || arms[0].ListPriceUSDPerMtok != 8 || arms[0].ListPriceOutputUSDPerMtok != 40 {
		t.Fatalf("stored = %q (%v)", raw, err)
	}
	body := a.get("/settings/jev-trial-panel").Body.String()
	if !strings.Contains(body, `<option value="/v1/chat/completions" selected`) || !strings.Contains(body, `value="40"`) {
		t.Errorf("the saved chat arm is not shown as selected with its output price: %s", body)
	}
}

// The same validation as the stored JSON: a chat arm without prices, an output price on
// another endpoint, and a path that is not on the list are refused and nothing is stored.
func TestChatEndpointArmIsRefusedWithoutItsPricesOrOnAnotherPath(t *testing.T) {
	for name, tc := range map[string]struct {
		form url.Values
		want string
	}{
		"no output price":           {chatRow(0, func(v url.Values, p string) { v.Set(p+"list_price_output_usd_per_mtok", "") }), "出力の定価"},
		"no input price":            {chatRow(0, func(v url.Values, p string) { v.Set(p+"list_price_usd_per_mtok", "") }), "定価"},
		"output price on systemone": {chatRow(0, func(v url.Values, p string) { v.Set(p+"endpoint", "") }), "出力の定価"},
		"host-relative path":        {chatRow(0, func(v url.Values, p string) { v.Set(p+"endpoint", "/api/v1/chat/completions") }), "/v1/chat/completions"},
		"output price is text":      {chatRow(0, func(v url.Values, p string) { v.Set(p+"list_price_output_usd_per_mtok", "x") }), "出力の定価は数値"},
	} {
		a := newApp(t)
		rec := a.post("/settings/jev-trial", join(url.Values{"enabled": {"on"}}, tc.form), true)
		if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), tc.want) {
			t.Errorf("%s: %d %s, want 422 mentioning %q", name, rec.Code, rec.Body.String(), tc.want)
		}
		for _, key := range jevtrialsettings.Keys() {
			if _, ok := a.stored(key); ok {
				t.Errorf("%s: stored %s", name, key)
			}
		}
	}
}
