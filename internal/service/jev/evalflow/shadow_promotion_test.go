package evalflow_test

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain/jevshadow"

	"github.com/ousiassllc/pitha-trador/internal/config/jevshadowsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/service/jev"
	"github.com/ousiassllc/pitha-trador/internal/service/jevprompt"
)

func TestShadow_CostSumsAreDailyInJSTAndCumulative(t *testing.T) {
	e := newShadowEnv(t, metalabelsettings.ModePaper, &fakeJev{})
	e.enable()
	e.evaluate()
	e.shadow.Wait()
	e.now = e.now.Add(24 * time.Hour)
	e.evaluate()
	e.evaluate()
	e.shadow.Wait()

	from, to := jevshadowsettings.DayBounds(e.now)
	today, err := e.results.SpentBetween(context.Background(), from, to)
	if err != nil {
		t.Fatal(err)
	}
	total, err := e.results.TotalSpent(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	perCall := jev.InputCostUSD(1200)
	if today.Calls != 2 || total.Calls != 3 || !near(today.CostUSD, 2*perCall) || !near(total.CostUSD, 3*perCall) {
		t.Fatalf("today=%+v total=%+v, want 2 and 3 calls of %v", today, total, perCall)
	}
	days, err := e.results.DailyCosts(context.Background(), 7)
	if err != nil || len(days) != 2 || days[0].Date != "2026-10-23" || days[0].Calls != 2 || days[1].Date != "2026-10-22" {
		t.Fatalf("daily costs = %+v, %v", days, err)
	}
}

func TestPromotion_SwitchesTheLiveVersionAndIsAudited(t *testing.T) {
	e := newShadowEnv(t, metalabelsettings.ModePaper, &fakeJev{})
	e.enable()
	if got := jev.LiveVersion(context.Background(), e.settings); got != "trader-v3" {
		t.Fatalf("live version before = %q", got)
	}

	promote := func(actor, client, version string) (jevshadow.Promotion, error) {
		tx, err := e.db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		p, err := jevprompt.Promote(context.Background(), tx, e.now, actor, client, version)
		if err != nil {
			_ = tx.Rollback()
			return p, err
		}
		return p, tx.Commit()
	}
	if _, err := promote(jevshadow.ActorGUI, "", "trader-v999"); err == nil {
		t.Fatal("promoting an unregistered version must fail")
	}
	if _, err := promote(jevshadow.ActorGUI, "", "trader-v3"); err == nil {
		t.Fatal("promoting the live version must fail")
	}
	if n := e.count(`SELECT COUNT(*) FROM jev_prompt_promotions`); n != 0 {
		t.Fatalf("a refused promotion left %d audit rows", n)
	}

	p, err := promote(jevshadow.ActorMCP, "100.64.0.9", "trader-v4")
	if err != nil {
		t.Fatalf("promote: %v", err)
	}
	if p.FromVersion != "trader-v3" || p.ToVersion != "trader-v4" || p.Actor != "mcp" || p.Client != "100.64.0.9" {
		t.Fatalf("promotion = %+v", p)
	}
	if got := jev.LiveVersion(context.Background(), e.settings); got != "trader-v4" {
		t.Fatalf("live version after = %q", got)
	}
	var from, to, actor, client string
	if err := e.db.QueryRow(`SELECT from_version, to_version, actor, client FROM jev_prompt_promotions`).Scan(&from, &to, &actor, &client); err != nil ||
		from != "trader-v3" || to != "trader-v4" || actor != "mcp" || client != "100.64.0.9" {
		t.Fatalf("audit row = %s %s %s %s, %v", from, to, actor, client, err)
	}

	// The Trader now sends and records the promoted version, and a shadow whose
	// candidate equals the live version has nothing to compare.
	live := e.evaluate()
	e.shadow.Wait()
	if live.QuestionVersion != "trader-v4" || e.api.calls("trader-v4") != 1 || e.api.calls("trader-v3") != 0 {
		t.Fatalf("after promotion: recorded %q, calls v3=%d v4=%d", live.QuestionVersion, e.api.calls("trader-v3"), e.api.calls("trader-v4"))
	}
	if n := e.count(`SELECT COUNT(*) FROM jev_shadow_decisions`); n != 0 {
		t.Fatalf("shadow rows = %d, want 0 (candidate == live)", n)
	}
	if _, err := promote(jevshadow.ActorGUI, "", "trader-v4"); err == nil {
		t.Fatal("promoting the now-live version again must fail")
	}
}

func TestLiveVersion_StoredUnknownVersionFallsBackToTheBuildDefault(t *testing.T) {
	e := newShadowEnv(t, metalabelsettings.ModePaper, &fakeJev{})
	e.set(jevshadowsettings.KeyLiveVersion, `"trader-from-the-future"`)
	if got := jev.LiveVersion(context.Background(), e.settings); got != jev.TraderQuestionVersion {
		t.Fatalf("LiveVersion = %q, want the build default for an unregistered stored version", got)
	}
	e.set(jevshadowsettings.KeyLiveVersion, `not json`)
	if got := jev.LiveVersion(context.Background(), e.settings); got != jev.TraderQuestionVersion {
		t.Fatalf("LiveVersion = %q, want the build default for a damaged value", got)
	}
}

func TestDecisionInsertRefusesAMissingPromptVersion(t *testing.T) {
	db := newTestDB(t)
	inst := mustCreateInstrument(t, market.NewInstrumentRepository(db), "1301")
	_, err := judgement.NewDecisionRepository(db).Insert(context.Background(), domain.JevDecision{
		InstrumentID: inst.ID, Symbol: "1301", Timestamp: time.Now(), DecisionType: domain.JevDecisionTypeTrader,
		StateHash: "h", StateJSON: "{}", ResponseJSON: "{}", ModelID: "m",
	})
	if err == nil {
		t.Fatal("a decision without a prompt version must be refused")
	}
}
