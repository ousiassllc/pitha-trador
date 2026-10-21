package mcpwritesettings_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/mcpwritesettings"
)

// The whitelist is exactly the keys #806 names. A change here is a decision
// about what the MCP may write, so it is pinned verbatim.
func TestWhitelistIsExactlyTheDecidedKeys(t *testing.T) {
	want := []string{
		"risk.paper.initial_capital_jpy", "risk.paper.max_position_per_symbol_pct", "risk.paper.max_total_exposure_pct", "risk.paper.max_trade_loss_pct",
		"risk.bucket.stock.budget_jpy", "risk.bucket.bond_etf.budget_jpy",
		"risk.operating_capital_jpy", "risk.daily_loss_limit_pct", "risk.profit_lock.trigger_pct", "risk.profit_lock.giveback_pct", "risk.max_daily_trades",
		"risk.daily_loss_limit_jpy", "risk.profit_lock.trigger_jpy",
		"metalabel.llm_cutoff", "metalabel.llm_cutoff_model", "metalabel.llm_cutoff_confirmed_on", "metalabel.llm_cutoff_source",
	}
	if len(want) != 17 {
		t.Fatalf("the test lists %d keys, issue #824 makes the whitelist 17", len(want))
	}
	if got := mcpwritesettings.Keys(); !slices.Equal(got, want) {
		t.Fatalf("whitelist = %v\nwant %v", got, want)
	}
	for _, s := range mcpwritesettings.Specs() {
		if s.Unit == "" || s.Description == "" || s.Constraint == "" {
			t.Errorf("%s: spec is incomplete: %+v", s.Key, s)
		}
	}
}

// Nothing dangerous is whitelisted: no Live value, bootstrap toggle, secret,
// order / transfer / kill-switch key, nor the write switch itself.
func TestWhitelistHoldsNothingDangerous(t *testing.T) {
	banned := []string{"live", "bootstrap", "secret", "password", "token", "api_key", "private_key", "order", "transfer", "withdraw",
		"kill", "release", "broker", "system.", "mcp."}
	for _, key := range mcpwritesettings.Keys() {
		for _, b := range banned {
			if strings.Contains(key, b) {
				t.Errorf("whitelisted key %q contains %q", key, b)
			}
		}
		if key == mcpwritesettings.KeyWriteEnabled {
			t.Errorf("the write switch is whitelisted as a setting")
		}
	}
}

// TestWhitelistHoldsNoSecret pins the masking policy's premise: no whitelisted
// key is secret-class, so values are logged as-is; a key that were, or a key
// off the whitelist, is masked by default.
func TestWhitelistHoldsNoSecret(t *testing.T) {
	for _, s := range mcpwritesettings.Specs() {
		if s.Secret || mcpwritesettings.IsSecret(s.Key) {
			t.Errorf("%s is secret-class: the whitelist must hold none", s.Key)
		}
		if got := mcpwritesettings.Mask(s.Key, "123"); got != "123" {
			t.Errorf("Mask(%s) = %q, want the value as-is", s.Key, got)
		}
	}
	for _, key := range []string{"broker.tachibana.password", "secrets.api_key", "anything.unknown", "", "risk.live.max_position_per_symbol_pct"} {
		if !mcpwritesettings.IsSecret(key) {
			t.Errorf("%q is not treated as secret", key)
		}
		if got := mcpwritesettings.Mask(key, "hunter2"); got != mcpwritesettings.Masked {
			t.Errorf("Mask(%q) = %q, want %q", key, got, mcpwritesettings.Masked)
		}
	}
}

func TestLookupAndIsAllowed(t *testing.T) {
	if !mcpwritesettings.IsAllowed("risk.paper.max_trade_loss_pct") || mcpwritesettings.IsAllowed("risk.paper.max_trade_loss_pct ") ||
		mcpwritesettings.IsAllowed(mcpwritesettings.KeyWriteEnabled) || mcpwritesettings.IsAllowed("paper.bootstrap_entry_enabled") {
		t.Fatal("IsAllowed disagrees with the whitelist")
	}
	if _, ok := mcpwritesettings.Lookup("nope"); ok {
		t.Fatal("Lookup found an unknown key")
	}
	// Specs returns a copy: editing it must not widen the whitelist.
	specs := mcpwritesettings.Specs()
	specs[0].Key = "evil"
	if mcpwritesettings.IsAllowed("evil") {
		t.Fatal("Specs exposes the internal slice")
	}
}

func TestApplyWriteSwitch(t *testing.T) {
	for raw, want := range map[string]bool{"1": true, "0": false} {
		if got, err := mcpwritesettings.Apply(raw); err != nil || got != want {
			t.Errorf("Apply(%s) = %v, %v", raw, got, err)
		}
	}
	for _, raw := range []string{"2", "-1", `"1"`, "true", "", "null"} {
		if _, err := mcpwritesettings.Apply(raw); err == nil {
			t.Errorf("Apply(%q) accepted", raw)
		}
	}
	if mcpwritesettings.Text(true) != "1" || mcpwritesettings.Text(false) != "0" {
		t.Error("Text")
	}
}

// The order-proposal switch (issue #827) is not a setting the MCP can write:
// settings_set refuses it, and it is off unless the Settings screen stored 1.
func TestOrderProposalsSwitchIsNotWhitelistedAndDefaultsOff(t *testing.T) {
	if mcpwritesettings.IsAllowed(mcpwritesettings.KeyOrderProposalsEnabled) {
		t.Fatal("mcp.order_proposals_enabled is writable as a setting")
	}
	if slices.Contains(mcpwritesettings.Keys(), mcpwritesettings.KeyOrderProposalsEnabled) {
		t.Fatal("mcp.order_proposals_enabled is in Keys")
	}
	if mcpwritesettings.DefaultOrderProposalsEnabled {
		t.Fatal("order proposals are on by default")
	}
	for raw, want := range map[string]bool{"1": true, "0": false} {
		if got, err := mcpwritesettings.ApplyOrderProposals(raw); err != nil || got != want {
			t.Errorf("ApplyOrderProposals(%s) = %v, %v", raw, got, err)
		}
	}
	for _, raw := range []string{"2", "-1", `"1"`, "true", "", "null"} {
		if _, err := mcpwritesettings.ApplyOrderProposals(raw); err == nil {
			t.Errorf("ApplyOrderProposals(%q) accepted", raw)
		}
	}
}
