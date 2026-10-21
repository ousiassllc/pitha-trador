package jevpromptroute_test

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/jevshadowsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
)

func TestPromotionIsExplicitAuditedAndShownInThePanel(t *testing.T) {
	a := newApp(t)
	if rec := a.post("/settings/jev-prompt/promote", url.Values{"version": {"trader-v4"}}, false); rec.Code == http.StatusOK {
		t.Fatalf("promotion without CSRF = %d", rec.Code)
	}
	if n := a.rows(`SELECT COUNT(*) FROM jev_prompt_promotions`); n != 0 {
		t.Fatalf("promotion rows = %d before any promotion", n)
	}
	// Saving the shadow settings is not a promotion.
	a.post("/settings/jev-prompt/shadow", url.Values{"enabled": {"on"}, "candidate": {"trader-v4"}, "cap": {"1"}}, true)
	if _, ok := a.stored(jevshadowsettings.KeyLiveVersion); ok || a.rows(`SELECT COUNT(*) FROM jev_prompt_promotions`) != 0 {
		t.Fatal("enabling the shadow with a candidate changed the live version")
	}

	rec := a.post("/settings/jev-prompt/promote", url.Values{"version": {"trader-v4"}}, true)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "trader-v3 から trader-v4 に昇格しました") {
		t.Fatalf("promote = %d %s", rec.Code, rec.Body.String())
	}
	if v, _ := a.stored(jevshadowsettings.KeyLiveVersion); v != `"trader-v4"` {
		t.Fatalf("live version = %q", v)
	}
	var actor, client, from, to string
	if err := a.db.QueryRow(`SELECT actor, client, from_version, to_version FROM jev_prompt_promotions`).Scan(&actor, &client, &from, &to); err != nil {
		t.Fatal(err)
	}
	if actor != "gui" || client != "" || from != "trader-v3" || to != "trader-v4" {
		t.Fatalf("audit row = %s %q %s %s", actor, client, from, to)
	}
	body := a.get("/settings/jev-prompt-panel").Body.String()
	for _, want := range []string{`data-testid="jev-prompt-live-version">trader-v4<`, `data-testid="jev-prompt-promotion-row"`, "GUI", "trader-v3", "trader-v4"} {
		if !strings.Contains(body, want) {
			t.Errorf("panel after promotion lacks %q", want)
		}
	}
	// The live version is no longer offered as a candidate / target; trader-v3 is now.
	if strings.Contains(body, `<option value="trader-v4"`) {
		t.Error("the new live version is still offered")
	}

	for _, version := range []string{"trader-v4", "trader-v999", ""} {
		if rec := a.post("/settings/jev-prompt/promote", url.Values{"version": {version}}, true); rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("promote %q = %d, want 422", version, rec.Code)
		}
	}
	if n := a.rows(`SELECT COUNT(*) FROM jev_prompt_promotions`); n != 1 {
		t.Errorf("promotion rows = %d, want 1", n)
	}
}

func TestPanelShowsTheCostAndWhyTheShadowStopped(t *testing.T) {
	a := newApp(t)
	inst, err := market.NewInstrumentRepository(a.db).Create(context.Background(), domain.Instrument{Symbol: "7203", Name: "7203 Inc", Market: "TSE Prime", IsActive: true})
	if err != nil {
		t.Fatal(err)
	}
	decisions := judgement.NewDecisionRepository(a.db)
	for _, row := range []struct {
		at   string
		cost float64
	}{{"2026-10-21T14:00:00Z", 0.02}, {"2026-10-22T00:30:00Z", 0.03}, {"2026-10-22T01:30:00Z", 0.01}} {
		live, err := decisions.Insert(context.Background(), domain.JevDecision{
			InstrumentID: inst.ID, Symbol: "7203", Timestamp: now, DecisionType: domain.JevDecisionTypeTrader,
			StateHash: "h", StateJSON: "{}", QuestionVersion: "trader-v3", ResponseJSON: "{}", ModelID: "m",
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := a.db.Exec(`INSERT INTO jev_shadow_decisions (jev_decision_id, prompt_version, model_id, response_json, direction, regime, entry_quality,
			confidence, toxic_flow, liquidity_stressed, continuation_probability, latency_ms, input_tokens, request_cost, created_at)
			VALUES (?, 'trader-v4', 'm', '{}', 'LONG', 'TREND', 'good', 0.5, 0, 0, 0, 1, 1000, ?, ?)`, live.ID, row.cost, row.at); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.settings.Set(context.Background(), jevshadowsettings.KeyHalt, `{"date":"2026-10-22","reason":"daily_cost_cap","spent_usd":0.04,"cap_usd":0.03,"at":"2026-10-22T01:00:00Z"}`, now); err != nil {
		t.Fatal(err)
	}
	body := a.get("/settings/jev-prompt-panel").Body.String()
	for _, want := range []string{
		`data-testid="jev-prompt-cost-today">0.04 USD`, `data-testid="jev-prompt-cost-total">0.06 USD`,
		"1 日の費用上限（0.03 USD）に達したため、今日のシャドーは止まっています", "ライブの判断は続いています",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("panel lacks %q\n%s", want, body)
		}
	}
	// A day stopped by the failure cap says so (and how many failures).
	if err := a.settings.Set(context.Background(), jevshadowsettings.KeyHalt, `{"date":"2026-10-22","reason":"daily_failure_cap","spent_usd":0,"cap_usd":0.1,"failures":20,"failure_cap":20,"at":"2026-10-22T01:00:00Z"}`, now); err != nil {
		t.Fatal(err)
	}
	if err := a.settings.Set(context.Background(), jevshadowsettings.KeyFailures, `{"date":"2026-10-22","count":20}`, now); err != nil {
		t.Fatal(err)
	}
	failBody := a.get("/settings/jev-prompt-panel").Body.String()
	for _, want := range []string{"失敗した呼び出しが 1 日の上限（20 回。暫定の案・未承認）に達したため、今日のシャドーは止まっています", `data-testid="jev-prompt-failure-cap"`, "今日: 20 回"} {
		if !strings.Contains(failBody, want) {
			t.Errorf("panel lacks %q\n%s", want, failBody)
		}
	}
	if n := strings.Count(body, `data-testid="jev-prompt-daily-row"`); n != 2 {
		t.Errorf("daily rows = %d, want 2 (2026-10-21 23:00 JST and 2026-10-22)", n)
	}
}
