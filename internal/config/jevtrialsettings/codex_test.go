package jevtrialsettings_test

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/jevtrialsettings"
)

// Issue #1081: nothing stored means the ChatGPT subscription provider is off with
// the default caps and no stop.
func TestLoadCodex_NothingStoredIsOff(t *testing.T) {
	c := jevtrialsettings.Load(context.Background(), fakeStore{}).Codex
	if c.Enabled || !c.CallCapIsDefault || !c.TokenCapIsDefault || c.CallCap != 30 || c.TokenCap != 500000 || c.Halt != nil || c.Login != nil {
		t.Errorf("Codex = %+v", c)
	}
}

func TestLoadCodex_DamagedValuesFallBackToTheSafeSide(t *testing.T) {
	c := jevtrialsettings.Load(context.Background(), fakeStore{
		jevtrialsettings.KeyCodexEnabled:       `"on"`,
		jevtrialsettings.KeyCodexDailyCallCap:  `0`,
		jevtrialsettings.KeyCodexDailyTokenCap: `2.5`,
		jevtrialsettings.KeyCodexHalt:          `{"reason":"","until":"x"}`,
		jevtrialsettings.KeyCodexLogin:         `{"state":"signed_in","at":"2026-11-13T00:00:00Z"}`,
	}).Codex
	if c.Enabled || !c.CallCapIsDefault || !c.TokenCapIsDefault || c.Halt != nil || c.Login != nil {
		t.Errorf("Codex = %+v, want off, default caps, no halt and no login", c)
	}
}

func TestLoadCodex_ReadsTheStoredValues(t *testing.T) {
	c := jevtrialsettings.Load(context.Background(), fakeStore{
		jevtrialsettings.KeyCodexEnabled:       `1`,
		jevtrialsettings.KeyCodexDailyCallCap:  `7`,
		jevtrialsettings.KeyCodexDailyTokenCap: `9000`,
		jevtrialsettings.KeyCodexHalt:          `{"reason":"usage_limit","until":"2026-11-13T09:00:00Z","until_known":true,"at":"2026-11-13T02:30:00Z"}`,
		jevtrialsettings.KeyCodexLogin:         `{"state":"logged_out","at":"2026-11-13T00:00:00Z"}`,
	}).Codex
	if !c.Enabled || c.CallCap != 7 || c.TokenCap != 9000 || c.CallCapIsDefault || c.TokenCapIsDefault {
		t.Errorf("Codex = %+v", c)
	}
	if c.Halt == nil || c.Halt.Reason != jevtrialsettings.CodexHaltUsageLimit || !c.Halt.UntilKnown || c.Login == nil || c.Login.State != jevtrialsettings.CodexLoginLoggedOut {
		t.Errorf("halt %+v login %+v", c.Halt, c.Login)
	}
	at := time.Date(2026, 11, 13, 8, 59, 59, 0, time.UTC)
	if !c.Halt.ActiveAt(at) || c.Halt.ActiveAt(at.Add(time.Second)) {
		t.Error("the stop must end exactly at its until time")
	}
}

func TestCodexKeysAreTrialKeys(t *testing.T) {
	for _, k := range []string{jevtrialsettings.KeyCodexEnabled, jevtrialsettings.KeyCodexDailyCallCap, jevtrialsettings.KeyCodexDailyTokenCap,
		jevtrialsettings.KeyCodexHalt, jevtrialsettings.KeyCodexLogin} {
		if !jevtrialsettings.IsKey(k) {
			t.Errorf("%s is not a trial key", k)
		}
	}
}

func TestCodexArmValidation(t *testing.T) {
	ok := jevtrialsettings.Arm{ID: "cx", Provider: jevtrialsettings.ProviderCodexChatGPT, Model: "gpt-6.1-sol", PromptVersion: "trader-v3"}
	if err := ok.Validate(); err != nil {
		t.Fatalf("a plain Codex arm: %v", err)
	}
	for name, mut := range map[string]func(*jevtrialsettings.Arm){
		"provider_only":   func(a *jevtrialsettings.Arm) { a.ProviderOnly = []string{"x"} },
		"data_collection": func(a *jevtrialsettings.Arm) { a.DataCollection = "allow" },
		"zdr":             func(a *jevtrialsettings.Arm) { a.ZDR = true },
		"a USD cap":       func(a *jevtrialsettings.Arm) { a.DailyCostCapUSD = 0.05 },
		"a list price":    func(a *jevtrialsettings.Arm) { a.ListPriceUSDPerMtok = 1 },
	} {
		a := ok
		mut(&a)
		if err := a.Validate(); err == nil {
			t.Errorf("a Codex arm with %s was accepted: its cost is not USD", name)
		}
	}
}

func TestValidateCodexCaps(t *testing.T) {
	for _, bad := range [][2]int{{0, 1}, {1, 0}, {-1, 5}, {jevtrialsettings.MaxCodexDailyCallCap + 1, 5}, {5, jevtrialsettings.MaxCodexDailyTokenCap + 1}} {
		if err := jevtrialsettings.ValidateCodexCaps(bad[0], bad[1]); err == nil {
			t.Errorf("caps %v accepted", bad)
		}
	}
	if err := jevtrialsettings.ValidateCodexCaps(1, 1); err != nil {
		t.Error(err)
	}
}
