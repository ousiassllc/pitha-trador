package jevtrialsettings_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/jevtrialsettings"
)

type fakeStore map[string]string

func (f fakeStore) Get(_ context.Context, key string) (string, bool, error) {
	if key == "boom" {
		return "", false, errors.New("boom")
	}
	v, ok := f[key]
	return v, ok, nil
}

func TestLoad_NothingStoredIsOffWithNoArmsAndTheDefaultCap(t *testing.T) {
	s := jevtrialsettings.Load(context.Background(), fakeStore{})
	if s.Enabled || len(s.Arms) != 0 || s.Halt != nil || !s.CapIsDefault || s.CapUSD != 0.10 {
		t.Errorf("Load(empty) = %+v, want off, no arms, no halt and the default 0.10 cap", s)
	}
}

func TestLoad_DamagedValuesFallBackToTheSafeSide(t *testing.T) {
	s := jevtrialsettings.Load(context.Background(), fakeStore{
		jevtrialsettings.KeyEnabled:         `"yes"`,
		jevtrialsettings.KeyArms:            `{"not":"an array"}`,
		jevtrialsettings.KeyDailyCostCapUSD: `-1`,
		jevtrialsettings.KeyHalt:            `garbage`,
	})
	if s.Enabled || len(s.Arms) != 0 || s.ArmsError == "" || !s.CapIsDefault || s.Halt != nil {
		t.Errorf("Load(damaged) = %+v, want off, no arms with an error, the default cap and no halt", s)
	}
}

func TestLoad_ReadsTheStoredValues(t *testing.T) {
	s := jevtrialsettings.Load(context.Background(), fakeStore{
		jevtrialsettings.KeyEnabled:         `1`,
		jevtrialsettings.KeyArms:            `[{"id":"or-clef-flash-v3","provider":"openrouter","model":"cloudflare/clef-flash","prompt_version":"trader-v3"}]`,
		jevtrialsettings.KeyDailyCostCapUSD: `0.25`,
	})
	if !s.Enabled || len(s.Arms) != 1 || s.Arms[0].ID != "or-clef-flash-v3" || s.CapUSD != 0.25 || s.CapIsDefault {
		t.Errorf("Load = %+v", s)
	}
	a := s.Arms[0]
	if a.DataCollectionOrDefault() != "deny" || a.CapUSD() != jevtrialsettings.DefaultArmDailyCostCapUSD {
		t.Errorf("arm defaults = data_collection %q cap %v, want deny and the default arm cap", a.DataCollectionOrDefault(), a.CapUSD())
	}
}

func TestParseArms_RejectsInvalidArms(t *testing.T) {
	valid := `{"id":"a","provider":"openrouter","model":"m","prompt_version":"trader-v3"}`
	for name, tc := range map[string]struct{ in, want string }{
		"not an array":        {`{}`, "array"},
		"unknown field":       {`[{"id":"a","provider":"openrouter","model":"m","prompt_version":"trader-v3","modle":"x"}]`, "unknown field"},
		"bad id (upper case)": {`[{"id":"Arm","provider":"openrouter","model":"m","prompt_version":"trader-v3"}]`, "arm id"},
		"bad id (space)":      {`[{"id":"a b","provider":"openrouter","model":"m","prompt_version":"trader-v3"}]`, "arm id"},
		"other provider":      {`[{"id":"a","provider":"cloudflare","model":"m","prompt_version":"trader-v3"}]`, "provider"},
		"no model":            {`[{"id":"a","provider":"openrouter","prompt_version":"trader-v3"}]`, "model"},
		"no prompt version":   {`[{"id":"a","provider":"openrouter","model":"m"}]`, "prompt version"},
		"cap above max":       {`[{"id":"a","provider":"openrouter","model":"m","prompt_version":"trader-v3","daily_cost_cap_usd":1000}]`, "daily_cost_cap_usd"},
		"negative list price": {`[{"id":"a","provider":"openrouter","model":"m","prompt_version":"trader-v3","list_price_usd_per_mtok":-1}]`, "list_price"},
		"bad data collection": {`[{"id":"a","provider":"openrouter","model":"m","prompt_version":"trader-v3","data_collection":"maybe"}]`, "data_collection"},
		"duplicate ids":       {`[` + valid + `,` + valid + `]`, "twice"},
		"too many arms":       {"[" + strings.Repeat(valid+",", jevtrialsettings.MaxArms) + valid + "]", "at most"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := jevtrialsettings.ParseArms(tc.in); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("ParseArms error = %v, want one mentioning %q", err, tc.want)
			}
		})
	}
	if arms, err := jevtrialsettings.ParseArms(`[]`); err != nil || len(arms) != 0 {
		t.Errorf("ParseArms([]) = %v, %v, want no arms and no error", arms, err)
	}
}

func TestEncodeArms_RoundTrips(t *testing.T) {
	in := []jevtrialsettings.Arm{{ID: "or-clef-v3", Provider: "openrouter", Model: "cloudflare/clef", PromptVersion: "trader-v3", DailyCostCapUSD: 0.03,
		ListPriceUSDPerMtok: 0.24, ProviderOnly: []string{"deepinfra"}, DataCollection: "deny", ZDR: true}}
	enc, err := jevtrialsettings.EncodeArms(in)
	if err != nil {
		t.Fatalf("EncodeArms: %v", err)
	}
	out, err := jevtrialsettings.ParseArms(enc)
	if err != nil || len(out) != 1 || out[0].ProviderOnly[0] != "deepinfra" || !out[0].ZDR || out[0].ListPriceUSDPerMtok != 0.24 {
		t.Errorf("round trip = %+v, %v", out, err)
	}
	if enc, err := jevtrialsettings.EncodeArms(nil); err != nil || enc != "[]" {
		t.Errorf("EncodeArms(nil) = %q, %v, want []", enc, err)
	}
}

func TestKeys_NoneIsAShadowKeyAndPerArmKeysCarryTheArm(t *testing.T) {
	for _, k := range append(jevtrialsettings.Keys(), jevtrialsettings.HaltKey("a"), jevtrialsettings.FailuresKey("a"), jevtrialsettings.ThrottleKey("a")) {
		if strings.HasPrefix(k, "jev.shadow.") || !jevtrialsettings.IsKey(k) {
			t.Errorf("key %q must be a jev.trial.* key and never a jev.shadow.* one", k)
		}
	}
	if jevtrialsettings.HaltKey("a") != "jev.trial.halt.a" || jevtrialsettings.FailuresKey("a") != "jev.trial.failures.a" || jevtrialsettings.ThrottleKey("a") != "jev.trial.throttle.a" {
		t.Error("per-arm keys are not jev.trial.<halt|failures|throttle>.<arm>")
	}
}

func TestLoadArm_ReadsTheArmsOwnRecordsOnly(t *testing.T) {
	store := fakeStore{
		jevtrialsettings.HaltKey("a"):     `{"date":"2026-11-13","reason":"forbidden","status":403,"at":"2026-11-13T00:00:00Z"}`,
		jevtrialsettings.FailuresKey("a"): `{"date":"2026-11-13","count":3}`,
		jevtrialsettings.ThrottleKey("a"): `{"date":"2026-11-13","count":1,"status":429,"reason":"api_rate_limited","at":"2026-11-13T00:00:00Z","until":"2026-11-13T00:10:00Z"}`,
		jevtrialsettings.HaltKey("b"):     `{"date":"2026-11-13","reason":"arm_cost_cap","at":"2026-11-13T00:00:00Z"}`,
	}
	a := jevtrialsettings.LoadArm(context.Background(), store, "a")
	if !a.Halt.HaltOn("2026-11-13") || a.Halt.HaltOn("2026-11-14") || a.Failures.FailuresOn("2026-11-13") != 3 || a.Failures.FailuresOn("2026-11-14") != 0 {
		t.Errorf("LoadArm(a) = %+v / %+v / %+v", a.Halt, a.Failures, a.Throttle)
	}
	if !a.Throttle.ActiveAt(time.Date(2026, 11, 13, 0, 5, 0, 0, time.UTC)) || a.Throttle.ActiveAt(time.Date(2026, 11, 13, 0, 11, 0, 0, time.UTC)) {
		t.Error("the throttle cooldown does not end at until")
	}
	c := jevtrialsettings.LoadArm(context.Background(), store, "c")
	if c.Halt != nil || c.Failures != nil || c.Throttle != nil {
		t.Errorf("LoadArm(c) = %+v, want nothing: another arm's records must not leak", c)
	}
}
