package jevtrial_test

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/jevtrialsettings"
	svc "github.com/ousiassllc/pitha-trador/internal/service/jevtrial"
)

func TestSave_RejectedInputSavesNothing(t *testing.T) {
	bad := func(v float64) *float64 { return &v }
	good := arms(t, validArms)
	one := func(mutate func(a *jevtrialsettings.Arm)) []jevtrialsettings.Arm {
		a := jevtrialsettings.Arm{ID: "a", Provider: "openrouter", Model: "m", PromptVersion: "trader-v3"}
		mutate(&a)
		return []jevtrialsettings.Arm{a}
	}
	nine := make([]jevtrialsettings.Arm, jevtrialsettings.MaxArms+1)
	for i := range nine {
		nine[i] = jevtrialsettings.Arm{ID: "a" + strconv.Itoa(i), Provider: "openrouter", Model: "m", PromptVersion: "trader-v3"}
	}
	for name, form := range map[string]svc.Form{
		"unregistered prompt":        {Enabled: true, Arms: one(func(a *jevtrialsettings.Arm) { a.PromptVersion = "trader-v999" })},
		"empty prompt":               {Enabled: true, Arms: one(func(a *jevtrialsettings.Arm) { a.PromptVersion = "" })},
		"empty id":                   {Enabled: true, Arms: one(func(a *jevtrialsettings.Arm) { a.ID = "" })},
		"bad id":                     {Enabled: true, Arms: one(func(a *jevtrialsettings.Arm) { a.ID = "A b" })},
		"empty model":                {Enabled: true, Arms: one(func(a *jevtrialsettings.Arm) { a.Model = "" })},
		"other provider":             {Enabled: true, Arms: one(func(a *jevtrialsettings.Arm) { a.Provider = "x" })},
		"bad data collection":        {Enabled: true, Arms: one(func(a *jevtrialsettings.Arm) { a.DataCollection = "maybe" })},
		"negative arm cap":           {Enabled: true, Arms: one(func(a *jevtrialsettings.Arm) { a.DailyCostCapUSD = -1 })},
		"huge list price":            {Enabled: true, Arms: one(func(a *jevtrialsettings.Arm) { a.ListPriceUSDPerMtok = 1e6 })},
		"bad provider name":          {Enabled: true, Arms: one(func(a *jevtrialsettings.Arm) { a.ProviderOnly = []string{"a b"} })},
		"duplicate arm":              {Enabled: true, Arms: append(one(func(*jevtrialsettings.Arm) {}), one(func(*jevtrialsettings.Arm) {})...)},
		"nine arms":                  {Enabled: true, Arms: nine},
		"one bad among good":         {Enabled: true, Arms: append(append([]jevtrialsettings.Arm{}, good...), one(func(a *jevtrialsettings.Arm) { a.Model = "" })...)},
		"cap zero (with valid arms)": {Enabled: true, Arms: good, Cap: bad(0)},
		"cap negative":               {Enabled: true, Arms: good, Cap: bad(-1)},
		"cap huge":                   {Enabled: true, Arms: good, Cap: bad(1e9)},
	} {
		e := newEnv(t)
		err := e.s.Save(context.Background(), form)
		if !errors.Is(err, svc.ErrInvalidInput) {
			t.Errorf("%s: error = %v, want ErrInvalidInput", name, err)
		}
		for _, key := range jevtrialsettings.Keys() {
			if _, ok := e.stored(t, key); ok {
				t.Errorf("%s: stored %s", name, key)
			}
		}
	}
}

// The form and the stored JSON share one rule set: eight arms are accepted,
// and anything the stored setting would not parse the form does not save either.
func TestValidateArms_SameRulesAsTheStoredJSON(t *testing.T) {
	eight := make([]jevtrialsettings.Arm, jevtrialsettings.MaxArms)
	for i := range eight {
		eight[i] = jevtrialsettings.Arm{ID: "a" + strconv.Itoa(i), Provider: "openrouter", Model: "m", PromptVersion: "trader-v3"}
	}
	value, err := svc.ValidateArms(eight)
	if err != nil {
		t.Fatalf("eight arms: %v", err)
	}
	if parsed, err := jevtrialsettings.ParseArms(value); err != nil || len(parsed) != jevtrialsettings.MaxArms {
		t.Fatalf("the value to store does not parse back: %q (%v)", value, err)
	}
	if value, err := svc.ValidateArms(nil); err != nil || value != "[]" {
		t.Errorf("no arms = %q, %v, want []", value, err)
	}
	for name, raw := range map[string]string{
		"bad id":         `[{"id":"A","provider":"openrouter","model":"m","prompt_version":"trader-v3"}]`,
		"no model":       `[{"id":"a","provider":"openrouter","model":"","prompt_version":"trader-v3"}]`,
		"bad provider":   `[{"id":"a","provider":"x","model":"m","prompt_version":"trader-v3"}]`,
		"bad collection": `[{"id":"a","provider":"openrouter","model":"m","prompt_version":"trader-v3","data_collection":"maybe"}]`,
		"duplicate":      `[{"id":"a","provider":"openrouter","model":"m","prompt_version":"trader-v3"},{"id":"a","provider":"openrouter","model":"m","prompt_version":"trader-v3"}]`,
	} {
		if _, err := jevtrialsettings.ParseArms(raw); err == nil {
			t.Fatalf("%s: the stored JSON accepts it", name)
		}
		var loose []jevtrialsettings.Arm
		if err := json.Unmarshal([]byte(raw), &loose); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.ValidateArms(loose); !errors.Is(err, svc.ErrInvalidInput) {
			t.Errorf("%s: form validation = %v, want ErrInvalidInput", name, err)
		}
	}
}
