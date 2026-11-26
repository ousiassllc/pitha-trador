package jevtrialsettings_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/jevtrialsettings"
)

func chatArm(mod func(*jevtrialsettings.Arm)) jevtrialsettings.Arm {
	a := jevtrialsettings.Arm{ID: "chat", Provider: "openrouter", Model: "openai/gpt-6.2-astra", PromptVersion: "trader-v3",
		Endpoint: jevtrialsettings.EndpointChatCompletions, ListPriceUSDPerMtok: 8, ListPriceOutputUSDPerMtok: 40}
	if mod != nil {
		mod(&a)
	}
	return a
}

// The chat endpoint is /v1/chat/completions (relative to the base URL, which ends in
// /api): the host-relative /api/v1/chat/completions would become .../api/api/v1/...,
// and any other path is not on the list.
func TestChatEndpoint_OnlyTheListedPathIsAccepted(t *testing.T) {
	if jevtrialsettings.EndpointChatCompletions != "/v1/chat/completions" {
		t.Fatalf("EndpointChatCompletions = %q", jevtrialsettings.EndpointChatCompletions)
	}
	if err := chatArm(nil).Validate(); err != nil {
		t.Fatalf("a chat arm with both prices: %v", err)
	}
	for _, ep := range []string{"/api/v1/chat/completions", "/v1/chat/completions/", "v1/chat/completions", "https://example.com/v1/chat/completions", "/v1/completions", "/V1/chat/completions"} {
		err := chatArm(func(a *jevtrialsettings.Arm) { a.Endpoint = ep }).Validate()
		var ae *jevtrialsettings.ArmsError
		if err == nil || !errors.As(err, &ae) || ae.Kind != jevtrialsettings.ArmsErrEndpoint {
			t.Errorf("endpoint %q: err = %v, want an endpoint error", ep, err)
		}
	}
	// A custom provider may call it too; Codex has no endpoint at all.
	if err := chatArm(func(a *jevtrialsettings.Arm) { a.Provider = "custom" }).Validate(); err != nil {
		t.Errorf("custom chat arm: %v", err)
	}
	codex := jevtrialsettings.Arm{ID: "codex", Provider: jevtrialsettings.ProviderCodexChatGPT, Model: "gpt-6.1-sol", PromptVersion: "trader-v3", Endpoint: jevtrialsettings.EndpointChatCompletions}
	if err := codex.Validate(); err == nil || !strings.Contains(err.Error(), "endpoint") {
		t.Errorf("a codex_chatgpt arm with the chat endpoint: err = %v, want it refused", err)
	}
}

// A chat arm prices its output as well as its input: both are required, and only a chat
// arm has an output price.
func TestChatArm_NeedsBothListPricesAndOnlyAChatArmHasAnOutputPrice(t *testing.T) {
	for name, tc := range map[string]struct {
		mod  func(*jevtrialsettings.Arm)
		kind string
	}{
		"no input price":      {func(a *jevtrialsettings.Arm) { a.ListPriceUSDPerMtok = 0 }, jevtrialsettings.ArmsErrListPrice},
		"no output price":     {func(a *jevtrialsettings.Arm) { a.ListPriceOutputUSDPerMtok = 0 }, jevtrialsettings.ArmsErrListPriceOutput},
		"negative output":     {func(a *jevtrialsettings.Arm) { a.ListPriceOutputUSDPerMtok = -1 }, jevtrialsettings.ArmsErrListPriceOutput},
		"huge output":         {func(a *jevtrialsettings.Arm) { a.ListPriceOutputUSDPerMtok = 1e6 }, jevtrialsettings.ArmsErrListPriceOutput},
		"output on systemone": {func(a *jevtrialsettings.Arm) { a.Endpoint = "" }, jevtrialsettings.ArmsErrListPriceOutput},
		"output on decisions": {func(a *jevtrialsettings.Arm) { a.Endpoint = jevtrialsettings.EndpointDecisions }, jevtrialsettings.ArmsErrListPriceOutput},
	} {
		err := chatArm(tc.mod).Validate()
		var ae *jevtrialsettings.ArmsError
		if err == nil || !errors.As(err, &ae) || ae.Kind != tc.kind {
			t.Errorf("%s: err = %v, want a %s error", name, err, tc.kind)
		}
	}
	// ParseArms (the stored value and settings_set) reads the new field and refuses it where it does not belong.
	arms, err := jevtrialsettings.ParseArms(`[{"id":"c","provider":"openrouter","model":"m","prompt_version":"trader-v3","endpoint":"/v1/chat/completions","list_price_usd_per_mtok":8,"list_price_output_usd_per_mtok":40}]`)
	if err != nil || arms[0].ListPriceOutputUSDPerMtok != 40 || !arms[0].IsChat() {
		t.Errorf("ParseArms chat arm = %+v, %v", arms, err)
	}
	if _, err := jevtrialsettings.ParseArms(`[{"id":"c","provider":"openrouter","model":"m","prompt_version":"trader-v3","endpoint":"/v1/chat/completions"}]`); err == nil {
		t.Error("a chat arm without prices was accepted")
	}
	// An arm that is not a chat arm stores no output price at all (the stored JSON of existing arms does not change).
	enc, err := jevtrialsettings.EncodeArms([]jevtrialsettings.Arm{{ID: "a", Provider: "openrouter", Model: "m", PromptVersion: "trader-v3", ListPriceUSDPerMtok: 0.24}})
	if err != nil || strings.Contains(enc, "output") {
		t.Errorf("encoded = %s, %v, want no output price", enc, err)
	}
}

// A chat arm is counted at the chat worst case unless its route is pinned; a stage-1
// arm's CountingUSDPerMtok and WorstCaseUSDPerMtok are not touched.
func TestChatCountingUSDPerMtok(t *testing.T) {
	const wi, wo = jevtrialsettings.ChatWorstInputUSDPerMtok, jevtrialsettings.ChatWorstOutputUSDPerMtok
	for name, tc := range map[string]struct {
		arm      jevtrialsettings.Arm
		wantIn   float64
		wantOut  float64
		stage1In float64
	}{
		"cheap prices, not pinned": {chatArm(func(a *jevtrialsettings.Arm) { a.ListPriceUSDPerMtok, a.ListPriceOutputUSDPerMtok = 2, 10 }), wi, wo, 2},
		"cheap prices, pinned": {chatArm(func(a *jevtrialsettings.Arm) {
			a.ListPriceUSDPerMtok, a.ListPriceOutputUSDPerMtok, a.ProviderOnly = 2, 10, []string{"openai"}
		}), 2, 10, 2},
		"dearer than the worst case": {chatArm(func(a *jevtrialsettings.Arm) { a.ListPriceUSDPerMtok, a.ListPriceOutputUSDPerMtok = 30, 180 }), 30, 180, 30},
		"unset prices":               {chatArm(func(a *jevtrialsettings.Arm) { a.ListPriceUSDPerMtok, a.ListPriceOutputUSDPerMtok = 0, 0 }), wi, wo, 0.24},
	} {
		in, out := tc.arm.ChatCountingUSDPerMtok()
		if in != tc.wantIn || out != tc.wantOut {
			t.Errorf("%s: ChatCountingUSDPerMtok = %v/%v, want %v/%v", name, in, out, tc.wantIn, tc.wantOut)
		}
		if got := tc.arm.CountingUSDPerMtok(); got != max(tc.stage1In, jevtrialsettings.WorstCaseUSDPerMtok) && !tc.arm.RoutePinned() {
			t.Errorf("%s: CountingUSDPerMtok = %v, the stage-1 rule moved", name, got)
		}
	}
	if jevtrialsettings.WorstCaseUSDPerMtok != 0.24 {
		t.Errorf("WorstCaseUSDPerMtok = %v, want 0.24 (the stage-1 counting is not changed by the chat arm)", jevtrialsettings.WorstCaseUSDPerMtok)
	}
}
