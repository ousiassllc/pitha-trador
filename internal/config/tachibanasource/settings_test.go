package tachibanasource_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/tachibanasource"
)

func TestNormalizeTachibanaSource_NightlyRunTimeRejectsDaytimeWindow(t *testing.T) {
	for raw, want := range map[string]string{
		"18:00": "18:00", "1:00": "01:00", "01:00": "01:00", "07:59": "07:59",
		"15:31": "15:31", "23:59": "23:59", "00:00": "00:00",
	} {
		got, err := tachibanasource.NormalizeTachibanaSourceSetting(tachibanasource.KeyTachibanaNightlyRunTime, raw)
		if err != nil || got != want {
			t.Errorf("run_time %q = %v, %v; want %q", raw, got, err, want)
		}
	}
	for _, raw := range []string{"08:00", "8:00", "09:00", "12:00", "15:00", "15:30", "abc", "25:00", "18:60", "18"} {
		if got, err := tachibanasource.NormalizeTachibanaSourceSetting(tachibanasource.KeyTachibanaNightlyRunTime, raw); err == nil {
			t.Errorf("run_time %q accepted as %v, want an error", raw, got)
		}
	}
	if !tachibanasource.TachibanaNightlyBlocked("08:00") || !tachibanasource.TachibanaNightlyBlocked("15:30") ||
		tachibanasource.TachibanaNightlyBlocked("07:59") || tachibanasource.TachibanaNightlyBlocked("15:31") {
		t.Error("TachibanaNightlyBlocked must cover exactly 08:00..15:30")
	}
}

func TestNormalizeTachibanaSource_RangesAndEnums(t *testing.T) {
	ok := map[string][]string{
		tachibanasource.KeyTachibanaCandidateSource:           {"daily_screen", "FIXED"},
		tachibanasource.KeyTachibanaNightlyRatePerSecond:      {"0.1", "1", "3"},
		tachibanasource.KeyTachibanaNightlyMinPrice:           {"0", "1000000"},
		tachibanasource.KeyTachibanaEventMaxConnects:          {"1", "10", "20"},
		tachibanasource.KeyTachibanaRestQuoteMinInterval:      {"10", "60", "3600"},
		tachibanasource.KeyTachibanaRestQuoteRequestsPerRound: {"1", "3"},
		tachibanasource.TachibanaScreenWeightKey("volume"):    {"0", "2.5", "100"},
		tachibanasource.TachibanaScreenTopNKey("volume"):      {"0", "10", "120"},
	}
	for key, raws := range ok {
		for _, raw := range raws {
			if _, err := tachibanasource.NormalizeTachibanaSourceSetting(key, raw); err != nil {
				t.Errorf("%s %q rejected: %v", key, raw, err)
			}
		}
	}
	bad := map[string][]string{
		tachibanasource.KeyTachibanaCandidateSource:           {"ranking", ""},
		tachibanasource.KeyTachibanaNightlyRatePerSecond:      {"0.09", "3.1", "abc", "NaN", "Inf"},
		tachibanasource.KeyTachibanaNightlyMinPrice:           {"-1", "1000001", "1.5"},
		tachibanasource.KeyTachibanaEventMaxConnects:          {"0", "21", "x"},
		tachibanasource.KeyTachibanaRestQuoteMinInterval:      {"9", "3601"},
		tachibanasource.KeyTachibanaRestQuoteRequestsPerRound: {"0", "4"},
		tachibanasource.TachibanaScreenWeightKey("volume"):    {"-0.1", "100.1", "NaN"},
		tachibanasource.TachibanaScreenTopNKey("volume"):      {"-1", "121", "1.5"},
	}
	for key, raws := range bad {
		for _, raw := range raws {
			if got, err := tachibanasource.NormalizeTachibanaSourceSetting(key, raw); err == nil {
				t.Errorf("%s %q accepted as %v, want an error", key, raw, got)
			}
		}
	}
	if _, err := tachibanasource.NormalizeTachibanaSourceSetting("broker.provider", "kabu"); err == nil {
		t.Error("a foreign key must be rejected")
	}
}

func TestNormalizeTachibanaSource_SymbolListsAndMarkets(t *testing.T) {
	got, err := tachibanasource.NormalizeTachibanaSourceSetting(tachibanasource.KeyTachibanaManualSymbols, " 7203, 6758\n130a、7203 ")
	if err != nil || got != "7203,6758,130A" {
		t.Errorf("manual symbols = %v, %v; want 7203,6758,130A (upper-cased, distinct)", got, err)
	}
	var many []string
	for i := 0; i < tachibanasource.MaxTachibanaWatchSymbols; i++ {
		many = append(many, "S"+string(rune('A'+i/26))+string(rune('A'+i%26)))
	}
	if _, err := tachibanasource.NormalizeTachibanaSourceSetting(tachibanasource.KeyTachibanaFixedSymbols, strings.Join(many, ",")); err != nil {
		t.Errorf("120 symbols rejected: %v", err)
	}
	if _, err := tachibanasource.NormalizeTachibanaSourceSetting(tachibanasource.KeyTachibanaFixedSymbols, strings.Join(many, ",")+",ZZZZ"); err == nil {
		t.Error("121 symbols accepted, want an error")
	}
	for _, raw := range []string{",", "7203.T", "^N225", "12345678901"} {
		if _, err := tachibanasource.NormalizeTachibanaSourceSetting(tachibanasource.KeyTachibanaManualSymbols, raw); err == nil {
			t.Errorf("symbols %q accepted", raw)
		}
	}
	if got, err := tachibanasource.NormalizeTachibanaSourceSetting(tachibanasource.KeyTachibanaNightlyMarkets, "Growth, prime,growth"); err != nil || got != "prime,growth" {
		t.Errorf("markets = %v, %v; want prime,growth", got, err)
	}
	if _, err := tachibanasource.NormalizeTachibanaSourceSetting(tachibanasource.KeyTachibanaNightlyMarkets, "prime,nyse"); err == nil {
		t.Error("an unknown market must be rejected")
	}
}

func TestBuildTachibanaSource_DefaultsAndKeys(t *testing.T) {
	s := tachibanasource.BuildTachibanaSource(nil)
	if s.CandidateSource != tachibanasource.TachibanaSourceDailyScreen || len(s.ManualSymbols) != 0 || len(s.FixedSymbols) != 0 ||
		s.Nightly.RunTime != "18:00" || s.Nightly.MaxPerSecond != 1 || !slices.Equal(s.Nightly.Markets, []string{"prime", "standard", "growth"}) ||
		s.Nightly.MinPriceJPY != 0 || len(s.Nightly.ExcludeSymbols) != 0 ||
		s.EventMaxConnectsPerDay != 10 || s.RestQuote.MinIntervalSeconds != 60 || s.RestQuote.RequestsPerRound != 1 ||
		len(s.Screen) != len(tachibanasource.TachibanaScreenIndicators) {
		t.Errorf("defaults = %+v", s)
	}
	for _, ind := range s.Screen {
		if ind.Weight != 1 || ind.TopN != 10 || !ind.Active() {
			t.Errorf("indicator default = %+v", ind)
		}
	}
	if err := s.Validate(); err != nil {
		t.Errorf("defaults must validate: %v", err)
	}
	// Every key has a default that its own normalizer accepts.
	for _, key := range tachibanasource.TachibanaSourceSettingKeys() {
		if !tachibanasource.IsTachibanaSourceSettingKey(key) {
			t.Errorf("%s is misclassified", key)
		}
		if def := tachibanasource.TachibanaSourceDefault(key); def != "" {
			if _, err := tachibanasource.NormalizeTachibanaSourceSetting(key, def); err != nil {
				t.Errorf("default %q of %s is rejected: %v", def, key, err)
			}
		}
	}
}

func TestTachibanaSourceSettings_ValidateNeedsAnActiveIndicator(t *testing.T) {
	text := map[string]string{}
	for _, ind := range tachibanasource.TachibanaScreenIndicators {
		text[tachibanasource.TachibanaScreenTopNKey(ind)] = "0"
	}
	if err := tachibanasource.BuildTachibanaSource(text).Validate(); err == nil {
		t.Error("all top_n = 0 must fail validation")
	}
	text[tachibanasource.TachibanaScreenTopNKey("turnover")] = "5"
	text[tachibanasource.TachibanaScreenWeightKey("turnover")] = "0"
	if err := tachibanasource.BuildTachibanaSource(text).Validate(); err == nil {
		t.Error("the only selecting indicator having weight 0 must fail validation")
	}
	text[tachibanasource.TachibanaScreenWeightKey("turnover")] = "0.5"
	if err := tachibanasource.BuildTachibanaSource(text).Validate(); err != nil {
		t.Errorf("one active indicator must validate: %v", err)
	}
}
