package codexflow_test

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/jevtrialsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain/jevtrial"
	"github.com/ousiassllc/pitha-trador/internal/service/jev"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/codexwire"
)

// Issue #1081: the ChatGPT subscription (Codex CLI) arm. The arms here are fakes
// that stand in for codexwire.Client: no CLI, login or network.

// The provider is off by default: an arm of it is never called, even with the
// trial on, and nothing is stored but the skip.
func TestCodexArmIsNotCalledWhileTheProviderSwitchIsOff(t *testing.T) {
	e := newEnv(t)
	a := e.addArm("cx", answer(codexResp(), 2000))
	e.enable(codexArm("cx"))
	e.evaluate()
	if a.calls() != 0 || len(e.rows()) != 0 {
		t.Fatalf("calls=%d rows=%d, want none while jev.trial.codex.enabled is off", a.calls(), len(e.rows()))
	}
	if e.skips("cx", jevtrial.SkipProviderDisabled) != 1 {
		t.Error("the skip must say the provider is disabled")
	}
}

// A subscription call has no USD: cost_source=subscription, cost_usd NULL, counted
// 0, tokens kept. The USD caps and another arm's USD figures are not touched.
func TestCodexCallIsStoredAsSubscriptionAndCountsZeroUSD(t *testing.T) {
	e := newEnv(t)
	e.addArm("cx", answer(codexResp(), 2000))
	costly := okResp()
	costly.ReportedCostUSD = new(0.004)
	e.addArm("or", answer(costly, 3000))
	e.enable(codexArm("cx"), defaultArm("or"))
	e.enableCodex("", "")
	e.evaluate()

	cx := e.rowOf("cx")
	if cx.Status != jevtrial.StatusOK || cx.CostSource != jevtrial.CostSubscription || cx.CostUSD != nil || cx.CountedCostUSD != 0 {
		t.Fatalf("codex row: status %s source %s cost %v counted %v", cx.Status, cx.CostSource, cx.CostUSD, cx.CountedCostUSD)
	}
	if cx.Provider != jevtrialsettings.ProviderCodexChatGPT || cx.InputTokens == nil || *cx.InputTokens != 1000 || cx.OutputTokens == nil || *cx.OutputTokens != 100 {
		t.Errorf("provider %q tokens %v/%v", cx.Provider, cx.InputTokens, cx.OutputTokens)
	}
	from, to := time.Date(2026, 11, 12, 15, 0, 0, 0, time.UTC), time.Date(2026, 11, 13, 15, 0, 0, 0, time.UTC)
	spent, err := e.results.SpentBetween(context.Background(), from, to)
	if err != nil {
		t.Fatal(err)
	}
	if spent.CountedCostUSD != 0.004 {
		t.Errorf("the day's counted USD = %v, want only the OpenRouter arm's 0.004", spent.CountedCostUSD)
	}
	if byArm, _ := e.results.SpentByArmBetween(context.Background(), "cx", from, to); byArm.CountedCostUSD != 0 {
		t.Errorf("the Codex arm counted %v USD, want 0", byArm.CountedCostUSD)
	}
}

// A failed call is stored as subscription too (no USD, counted 0) — it never
// counts a worst-case USD against the caps.
func TestCodexFailureCountsZeroUSD(t *testing.T) {
	e := newEnv(t)
	e.addArm("cx", failedCall(&codexwire.CallError{Reason: codexwire.ReasonExit}))
	e.enable(codexArm("cx"))
	e.enableCodex("", "")
	e.evaluate()
	d := e.rowOf("cx")
	if d.Status != jevtrial.StatusFailed || d.CostSource != jevtrial.CostSubscription || d.CostUSD != nil || d.CountedCostUSD != 0 {
		t.Fatalf("failed codex row = %+v", d)
	}
}

// The USD stops of the whole trial (the overall cap, an OpenRouter 402) are about
// USD and do not stop the subscription arm.
func TestCodexIgnoresTheWholeTrialUSDStops(t *testing.T) {
	e := newEnv(t)
	cx := e.addArm("cx", answer(codexResp(), 2000))
	e.addArm("or", failedCall(&jev.APIError{StatusCode: 402, Body: "insufficient credits"}))
	e.enable(codexArm("cx"), defaultArm("or"))
	e.enableCodex("", "")
	e.evaluate() // the OpenRouter arm's 402 stops the whole trial's USD side
	e.advance(time.Minute)
	e.evaluate()
	if cx.calls() != 2 {
		t.Fatalf("codex calls = %d, want 2: a 402 of another provider must not stop it", cx.calls())
	}
}

func TestCodexCallCapStopsTheProviderForTheDay(t *testing.T) {
	e := newEnv(t)
	a := e.addArm("cx", answer(codexResp(), 2000))
	e.enable(codexArm("cx"))
	e.enableCodex("2", "")
	e.evaluate()
	e.evaluate()
	e.evaluate() // the cap of 2 calls is reached
	if a.calls() != 2 {
		t.Fatalf("calls = %d, want 2", a.calls())
	}
	h := e.codexHalt()
	if h == nil || h.Reason != jevtrialsettings.CodexHaltCallCap || h.Calls != 2 {
		t.Fatalf("halt = %+v", h)
	}
	if e.skips("cx", jevtrial.SkipDailyCapCalls) != 1 {
		t.Error("the third comparison must be recorded as a daily_cap_calls skip")
	}
	e.advance(24 * time.Hour)
	e.evaluate()
	if a.calls() != 3 {
		t.Fatalf("calls = %d, want 3 after the JST day changed", a.calls())
	}
}

func TestCodexTokenCapStopsTheProviderForTheDay(t *testing.T) {
	e := newEnv(t)
	a := e.addArm("cx", answer(codexResp(), 2000)) // 1100 tokens a call
	e.enable(codexArm("cx"))
	e.enableCodex("100", "2000")
	e.evaluate()
	e.evaluate() // 2200 tokens: over the cap of 2000
	e.evaluate()
	if a.calls() != 2 {
		t.Fatalf("calls = %d, want 2", a.calls())
	}
	if h := e.codexHalt(); h == nil || h.Reason != jevtrialsettings.CodexHaltTokenCap || h.Tokens != 2200 {
		t.Fatalf("halt = %+v", h)
	}
}
