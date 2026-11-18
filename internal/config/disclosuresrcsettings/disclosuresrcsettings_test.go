package disclosuresrcsettings_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/disclosuresrcsettings"
)

type store map[string]string

func (s store) Get(_ context.Context, key string) (string, bool, error) {
	v, ok := s[key]
	return v, ok, nil
}

type failingStore struct{}

func (failingStore) Get(context.Context, string) (string, bool, error) {
	return "", false, errors.New("db down")
}

func TestNothingStoredMeansEverythingOff(t *testing.T) {
	s, err := disclosuresrcsettings.Load(context.Background(), store{})
	if err != nil {
		t.Fatal(err)
	}
	if s.EdinetEnabled || s.TachibanaNewsEnabled {
		t.Errorf("a source is on with nothing stored: %+v", s)
	}
	if s != disclosuresrcsettings.Default() {
		t.Errorf("Load of nothing = %+v, want Default %+v", s, disclosuresrcsettings.Default())
	}
	for _, key := range disclosuresrcsettings.Keys() {
		if _, err := disclosuresrcsettings.Parse(key, disclosuresrcsettings.DefaultText(key)); err != nil {
			t.Errorf("the default of %s does not parse: %v", key, err)
		}
	}
}

func TestStoredValuesApplyAndDamagedOnesFallBackToOff(t *testing.T) {
	stored := store{}
	stored[disclosuresrcsettings.KeyEdinetEnabled] = "1"
	stored[disclosuresrcsettings.KeyEdinetDailyRequestCap] = "12"
	stored[disclosuresrcsettings.KeyEdinetLookbackDays] = "30"
	stored[disclosuresrcsettings.KeyTachibanaNewsEnabled] = `"on"` // damaged: stays off
	stored[disclosuresrcsettings.KeyTachibanaNewsDailyRequestCap] = "5"
	stored[disclosuresrcsettings.KeyRetentionDays] = "9999" // out of range: the default
	s, err := disclosuresrcsettings.Load(context.Background(), stored)
	if err != nil {
		t.Fatal(err)
	}
	if !s.EdinetEnabled || s.EdinetDailyRequestCap != 12 || s.EdinetLookbackDays != 30 || s.TachibanaNewsDailyCap != 5 {
		t.Errorf("settings = %+v", s)
	}
	if s.TachibanaNewsEnabled {
		t.Error("a damaged switch turned the source on")
	}
	if s.RetentionDays != disclosuresrcsettings.DefaultRetentionDays {
		t.Errorf("retention = %d, want the default", s.RetentionDays)
	}
}

func TestAFailedReadIsAnErrorWithEverythingOff(t *testing.T) {
	s, err := disclosuresrcsettings.Load(context.Background(), failingStore{})
	if err == nil {
		t.Fatal("a failed read was not reported")
	}
	if s.EdinetEnabled || s.TachibanaNewsEnabled {
		t.Errorf("a failed read left a source on: %+v", s)
	}
}

func TestValidate(t *testing.T) {
	for _, c := range []struct {
		key string
		bad []string
	}{
		{disclosuresrcsettings.KeyEdinetEnabled, []string{"2", "-1", "0.5", "null", `"1"`, "true"}},
		{disclosuresrcsettings.KeyTachibanaNewsEnabled, []string{"2", "null"}},
		{disclosuresrcsettings.KeyEdinetDailyRequestCap, []string{"0", "1001", "1.5"}},
		{disclosuresrcsettings.KeyEdinetLookbackDays, []string{"0", "3651"}},
		{disclosuresrcsettings.KeyTachibanaNewsDailyRequestCap, []string{"0", "1001"}},
		{disclosuresrcsettings.KeyRetentionDays, []string{"29", "3651"}},
	} {
		for _, v := range c.bad {
			if err := disclosuresrcsettings.Validate(c.key, v); err == nil {
				t.Errorf("%s=%s was accepted", c.key, v)
			}
		}
	}
	for _, c := range [][2]string{
		{disclosuresrcsettings.KeyEdinetEnabled, "0"}, {disclosuresrcsettings.KeyEdinetEnabled, "1"},
		{disclosuresrcsettings.KeyEdinetDailyRequestCap, "1000"}, {disclosuresrcsettings.KeyRetentionDays, "30"},
	} {
		if err := disclosuresrcsettings.Validate(c[0], c[1]); err != nil {
			t.Errorf("%s=%s: %v", c[0], c[1], err)
		}
	}
	if err := disclosuresrcsettings.Validate("jev.trial.enabled", "1"); err == nil {
		t.Error("a key of another package was accepted")
	}
}
