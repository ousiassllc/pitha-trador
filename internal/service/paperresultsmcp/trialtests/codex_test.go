package trialtests

import (
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/jevtrialsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain/jevtrial"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

// Issue #1081: jev_trial_cost reports the ChatGPT subscription apart from every USD
// figure (calls and tokens, no USD), and the provider's switch, login state, caps
// and stop in limits.codex: reason codes and times only, never the CLI's text.
func TestCostReportsTheSubscriptionApartFromUSDAndTheCodexLimits(t *testing.T) {
	srv, w := newServer(t)
	seedCost(t, w)
	repo := judgement.NewTrialRepository(w)
	insert := func(decision int, source, status, failure string, in, out *int, created string) {
		t.Helper()
		ts, err := judgementTime(created)
		if err != nil {
			t.Fatal(err)
		}
		d := jevtrial.Decision{
			JevDecisionID: int64(decision), ArmID: "cx", PromptVersion: "trader-v3", Provider: jevtrialsettings.ProviderCodexChatGPT,
			RequestedModel: "gpt-6.1-sol", Status: status, FailureCode: failure, CostSource: source, InputTokens: in, OutputTokens: out,
			ErrorExcerpt: sentinelError, CreatedAt: ts,
		}
		if status == jevtrial.StatusOK {
			dir, eq, regime := "LONG", "fair", "TREND"
			d.Direction, d.EntryQuality, d.Regime = &dir, &eq, &regime
			d.ToxicFlow, d.LiquidityStressed, d.ContinuationProbability = f64(0.1), f64(0.1), f64(0.5)
		}
		if _, err := repo.Insert(t.Context(), d); err != nil {
			t.Fatal(err)
		}
	}
	n := func(v int) *int { return &v }
	insert(107, "subscription", "ok", "", n(1000), n(100), "2026-10-18T03:00:00Z")
	insert(108, "subscription", "ok", "", n(2000), n(300), "2026-10-18T04:00:00Z")
	insert(109, "subscription", "failed", "usage_limit", nil, nil, "2026-10-18T05:00:00Z")
	testkit.Setting(t, w, jevtrialsettings.KeyCodexEnabled, "1")
	testkit.Setting(t, w, jevtrialsettings.KeyCodexDailyCallCap, "5")
	testkit.Setting(t, w, jevtrialsettings.KeyCodexDailyTokenCap, "10000")
	testkit.Setting(t, w, jevtrialsettings.KeyCodexLogin, `{"state":"chatgpt","at":"2026-10-18T04:59:00Z"}`)
	testkit.Setting(t, w, jevtrialsettings.KeyCodexHalt, `{"reason":"usage_limit","until":"2099-01-01T00:00:00Z","until_known":true,"at":"2026-10-18T05:00:00Z"}`)

	c := cost(t, srv, map[string]any{})
	sub := armCost(t, c.PerArm, "cx").Subscription
	if sub.Calls != 3 || sub.InputTokens != 3000 || sub.OutputTokens != 400 {
		t.Errorf("subscription = %+v, want 3 calls and 3000 in / 400 out tokens", sub)
	}
	// Not one USD figure moved: the seeded USD totals are what they were.
	if c.Total.Reported.Calls != 3 || c.Total.Unknown.Calls != 2 || c.Total.Subscription.Calls != 3 || c.Total.Calls != 9 {
		t.Errorf("total = %+v", c.Total)
	}
	closeTo(t, "counted_total_usd", c.Total.CountedTotalUSD, 0.033+0.024+0.0048)
	closeTo(t, "overall counted today", c.Limits.OverallCountedTodayUSD, 0.02+0.024+0.0048)

	l := c.Limits.Codex
	if !l.Enabled || l.CallsToday != 3 || l.TokensToday != 3400 || l.DailyCallCap != 5 || l.DailyTokenCap != 10000 {
		t.Errorf("codex limits = %+v", l)
	}
	if l.Login.State != "chatgpt" || l.Halt == nil || l.Halt.Reason != "usage_limit" || !l.Halt.Active || !l.Halt.UntilKnown || l.Halt.Until != "2099-01-01T00:00:00Z" {
		t.Errorf("codex login %+v halt %+v", l.Login, l.Halt)
	}
	// The raw JSON carries no text of the CLI and no secret.
	raw := srv.call(t, "jev_trial_cost", map[string]any{}).body
	if strings.Contains(string(raw), sentinelError) || strings.Contains(string(raw), "auth.json") {
		t.Errorf("the report leaks text: %s", raw)
	}
}

func TestCostCodexDefaultsAreOff(t *testing.T) {
	srv, _ := newServer(t)
	l := cost(t, srv, map[string]any{}).Limits.Codex
	if l.Enabled || l.Halt != nil || l.Login.State != "" || l.DailyCallCap != jevtrialsettings.DefaultCodexDailyCallCap || l.DailyTokenCap != jevtrialsettings.DefaultCodexDailyTokenCap || l.CallsToday != 0 {
		t.Errorf("codex limits = %+v", l)
	}
}
