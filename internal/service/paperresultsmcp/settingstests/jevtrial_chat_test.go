package settingstests

import (
	"net/http"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/jevtrialsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
)

const armChat = `{"id":"or-chat","provider":"openrouter","model":"openai/gpt-6.2-astra","prompt_version":"trader-v3","endpoint":"/v1/chat/completions",` +
	`"list_price_usd_per_mtok":8,"list_price_output_usd_per_mtok":40,"daily_cost_cap_usd":0.5}`

// Issue #1224: settings_set takes a chat arm through the same validation as the Settings
// screen. No new key is added: only what jev.trial.arms accepts is wider.
func TestTrialSettingsSetAcceptsAChatArmAndRefusesTheRest(t *testing.T) {
	srv, w := settingsServer(t, nil)
	set(srv, t, "jev.trial.arms", arms(armChat)).data(t, "settings_set", &struct{}{})
	got, err := jevtrialsettings.ParseArms(storedTrial(t, w)["jev.trial.arms"])
	if err != nil || len(got) != 1 || !got[0].IsChat() || got[0].ListPriceOutputUSDPerMtok != 40 {
		t.Fatalf("stored arms = %+v (%v)", got, err)
	}
	for name, tc := range map[string]struct{ arm, want string }{
		"host-relative path":      {strings.Replace(armChat, `"/v1/chat/completions"`, `"/api/v1/chat/completions"`, 1), "API の送り先"},
		"arbitrary url":           {strings.Replace(armChat, `"/v1/chat/completions"`, `"https://example.com/v1/chat/completions"`, 1), "API の送り先"},
		"no output price":         {strings.Replace(armChat, `"list_price_output_usd_per_mtok":40,`, "", 1), "出力の定価"},
		"no input price":          {strings.Replace(armChat, `"list_price_usd_per_mtok":8,`, "", 1), "定価"},
		"output price, systemone": {strings.Replace(armChat, `"endpoint":"/v1/chat/completions",`, "", 1), "出力の定価"},
		"codex with chat path":    {`{"id":"cx","provider":"codex_chatgpt","model":"m","prompt_version":"trader-v3","endpoint":"/v1/chat/completions"}`, "codex_chatgpt"},
	} {
		r := set(srv, t, "jev.trial.arms", arms(tc.arm))
		r.wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidValue)
		if detail := r.errorBody(t).Error.Data["detail"]; !strings.Contains(detail, tc.want) {
			t.Errorf("%s: reason %q lacks %q", name, detail, tc.want)
		}
	}
	// The refused writes left the accepted chat arm in place and added nothing.
	if got, _ := jevtrialsettings.ParseArms(storedTrial(t, w)["jev.trial.arms"]); len(got) != 1 || got[0].ID != "or-chat" {
		t.Errorf("after refused writes: %+v", got)
	}
}
