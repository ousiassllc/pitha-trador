package jevtrial_test

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/jevtrialsettings"
	svc "github.com/ousiassllc/pitha-trador/internal/service/jevtrial"
)

// A rejected arm is worded in Japanese for the screen: no settings key, no Go error text.
func TestSave_InvalidArmsAreWordedForTheScreen(t *testing.T) {
	e := newEnv(t)
	for _, arm := range []jevtrialsettings.Arm{
		{ID: "A", Provider: "openrouter", Model: "m", PromptVersion: "trader-v3"},
		{ID: "a", Provider: "x", Model: "m", PromptVersion: "trader-v3"},
		{ID: "a", Provider: "openrouter", Model: "", PromptVersion: "trader-v3"},
		{ID: "a", Provider: "openrouter", Model: "m", PromptVersion: ""},
		{ID: "a", Provider: "openrouter", Model: "m", PromptVersion: "trader-v3", DataCollection: "maybe"},
		{ID: "a", Provider: "openrouter", Model: "m", PromptVersion: "trader-v3", DailyCostCapUSD: -1},
		{ID: "a", Provider: "openrouter", Model: "m", PromptVersion: "trader-v3", ListPriceUSDPerMtok: -1},
		{ID: "a", Provider: "openrouter", Model: "m", PromptVersion: "trader-v3", ProviderOnly: []string{"a b"}},
	} {
		err := e.s.Save(context.Background(), svc.Form{Arms: []jevtrialsettings.Arm{arm}})
		if !errors.Is(err, svc.ErrInvalidInput) {
			t.Fatalf("Save(%+v) = %v, want ErrInvalidInput", arm, err)
		}
		msg := strings.TrimPrefix(err.Error(), svc.ErrInvalidInput.Error()+": ")
		if strings.Contains(msg, "jev.trial") || strings.Contains(msg, "jevtrialsettings") || regexp.MustCompile(`\b(decode|must be|unknown field)\b|_usd|_version|provider_only|data_collection`).MatchString(msg) {
			t.Errorf("Save(%+v) message %q carries an internal name or English Go text", arm, msg)
		}
	}
}

// Turning the trial off is written before anything else, so a later failure cannot leave it on.
func TestSave_DisablingIsWrittenFirst(t *testing.T) {
	e := newEnv(t)
	if err := e.s.Save(context.Background(), svc.Form{Enabled: true, Arms: arms(t, validArms)}); err != nil {
		t.Fatal(err)
	}
	if err := e.s.Save(context.Background(), svc.Form{Enabled: false, Arms: arms(t, validArms)}); err != nil {
		t.Fatal(err)
	}
	st, err := e.s.State(context.Background())
	if err != nil || st.Enabled {
		t.Fatalf("State = %+v, %v, want the trial off", st, err)
	}
}

// A damaged stored arms setting is reported, and the form starts with no arm.
func TestState_DamagedArmsAreReported(t *testing.T) {
	e := newEnv(t)
	if err := e.settings.Set(context.Background(), "jev.trial.arms", `[{"id":"A"`, now); err != nil {
		t.Fatal(err)
	}
	st, err := e.s.State(context.Background())
	if err != nil || st.ArmsError == "" || len(st.Arms) != 0 {
		t.Fatalf("State = Arms %v ArmsError %q (%v), want no arm and an error", st.Arms, st.ArmsError, err)
	}
}
