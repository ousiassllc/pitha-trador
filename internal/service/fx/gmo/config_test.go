package gmo

import (
	"strings"
	"testing"
	"time"
)

func TestNewAppliesDefaults(t *testing.T) {
	c, err := New(Config{Symbols: []string{"USD_JPY"}})
	if err != nil {
		t.Fatal(err)
	}
	want := Config{
		BaseURL:        DefaultBaseURL,
		Symbols:        []string{"USD_JPY"},
		MinInterval:    time.Second,
		MaxAttempts:    5,
		InitialBackoff: time.Second,
		MaxBackoff:     30 * time.Second,
	}
	if got := c.cfg; got.BaseURL != want.BaseURL || got.MinInterval != want.MinInterval || got.MaxAttempts != want.MaxAttempts ||
		got.InitialBackoff != want.InitialBackoff || got.MaxBackoff != want.MaxBackoff {
		t.Fatalf("cfg = %+v, want %+v", got, want)
	}
}

func TestNewValidatesConfig(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr string
	}{
		{"no symbols", Config{}, "Symbols is empty"},
		{"lowercase symbol", Config{Symbols: []string{"usd_jpy"}}, "invalid symbol"},
		{"no underscore", Config{Symbols: []string{"USDJPY"}}, "invalid symbol"},
		{"interval below 1s", Config{Symbols: []string{"USD_JPY"}, MinInterval: 500 * time.Millisecond}, "below the 1s minimum"},
		{"negative interval", Config{Symbols: []string{"USD_JPY"}, MinInterval: -time.Second}, "below the 1s minimum"},
		{"negative attempts", Config{Symbols: []string{"USD_JPY"}, MaxAttempts: -1}, "MaxAttempts"},
		{"negative backoff", Config{Symbols: []string{"USD_JPY"}, InitialBackoff: -time.Second}, "backoff"},
		{"max backoff below initial", Config{Symbols: []string{"USD_JPY"}, InitialBackoff: 10 * time.Second, MaxBackoff: time.Second}, "backoff"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(tt.cfg)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestPairsAreConfigurable(t *testing.T) {
	syms := []string{"USD_JPY", "EUR_JPY", "AUD_NZD"}
	c, err := New(Config{Symbols: syms})
	if err != nil {
		t.Fatal(err)
	}
	syms[0] = "XXX_YYY" // the caller's slice must not leak into the Client
	got := c.Symbols()
	if strings.Join(got, ",") != "USD_JPY,EUR_JPY,AUD_NZD" {
		t.Fatalf("Symbols = %v", got)
	}
	got[0] = "ZZZ_ZZZ"
	if c.Symbols()[0] != "USD_JPY" {
		t.Fatal("Symbols must return a copy")
	}
}
