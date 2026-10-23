package evalflow_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain/jevshadow"

	"github.com/ousiassllc/pitha-trador/internal/config/jevshadowsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/service/jev"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/shadow"
	"github.com/ousiassllc/pitha-trador/internal/service/jevprompt"
	"github.com/ousiassllc/pitha-trador/internal/service/rag"
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
	perCall := shadow.InputCostUSD(1200)
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

// flakyStore reads runtime_settings through the real repository until fail is set.
type flakyStore struct {
	inner jevshadowsettings.Store
	fail  atomic.Bool
}

func (f *flakyStore) Get(ctx context.Context, key string) (string, bool, error) {
	if f.fail.Load() {
		return "", false, errors.New("settings read failed")
	}
	return f.inner.Get(ctx, key)
}

// A failed READ of the live version must never silently turn a promoted version
// back into the build default: the Trader keeps the last version it read, and
// with none read yet it fails the call (nothing is recorded or sent) instead of
// guessing.
func TestLiveVersion_ReadFailureKeepsTheLastVersionRead(t *testing.T) {
	e := newShadowEnv(t, metalabelsettings.ModePaper, &fakeJev{})
	e.set(jevshadowsettings.KeyLiveVersion, `"trader-v4"`) // a promotion happened
	store := &flakyStore{inner: e.settings}
	decisions := judgement.NewDecisionRepository(e.db)
	newTrader := func() *jev.Trader {
		return jev.NewTrader(e.client, decisions, rag.NewService(e.db, decisions, market.NewSnapshotRepository(e.db)), jev.WithPromptSettings(store))
	}
	state := jev.ScoutState{Symbol: "7203", Timestamp: e.now, Price: 2100}

	// Nothing read yet and the read fails: the call fails, nothing is sent or stored.
	store.fail.Store(true)
	fresh := newTrader()
	if _, err := fresh.Evaluate(context.Background(), e.instID, state); err == nil {
		t.Fatal("Evaluate succeeded although the live version could not be read")
	}
	if e.api.calls("trader-v3") != 0 || e.api.calls("trader-v4") != 0 || e.count(`SELECT COUNT(*) FROM jev_decisions`) != 0 {
		t.Fatal("a call was sent or recorded without a known live version")
	}

	// One good read, then the read fails: the promoted version stays.
	store.fail.Store(false)
	trader := newTrader()
	first, err := trader.Evaluate(context.Background(), e.instID, state)
	if err != nil || first.QuestionVersion != "trader-v4" {
		t.Fatalf("first decision = %+v, %v; want a trader-v4 one", first, err)
	}
	store.fail.Store(true)
	second, err := trader.Evaluate(context.Background(), e.instID, state)
	if err != nil {
		t.Fatalf("Evaluate after a failed read: %v", err)
	}
	if second.QuestionVersion != "trader-v4" || e.api.calls("trader-v3") != 0 {
		t.Fatalf("after a failed read the decision is %q (v3 calls %d); the promoted version must stay", second.QuestionVersion, e.api.calls("trader-v3"))
	}
}

// Close (the application's shutdown) cancels the calls in flight instead of
// waiting out their timeouts, and comparisons submitted afterwards are dropped.
func TestShadow_CloseCancelsCallsInFlightAndDropsLaterOnes(t *testing.T) {
	e := holdCandidate(t)
	closed := make(chan struct{})
	go func() { e.trader.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(10 * time.Second):
		t.Fatal("Close waited for the held shadow call instead of cancelling it")
	}
	e.evaluate()
	e.shadow.Wait()
	if got := e.api.calls("trader-v4"); got != 1 {
		t.Fatalf("candidate calls = %d, want 1 (a comparison submitted after Close is dropped)", got)
	}
}

// holdCandidate starts a comparison whose candidate call stays held at the API
// and returns once that call has arrived.
func holdCandidate(t *testing.T) *shadowEnv {
	t.Helper()
	hold := make(chan struct{})
	e := newShadowEnv(t, metalabelsettings.ModePaper, &fakeJev{holdShadow: hold})
	t.Cleanup(func() { close(hold) }) // registered after the server's Close, so it runs first: unblocks the held handler
	e.enable()
	e.evaluate()
	deadline := time.Now().Add(10 * time.Second)
	for e.api.calls("trader-v4") == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the candidate call never started")
		}
		time.Sleep(5 * time.Millisecond)
	}
	return e
}

// A call cut off by Close is neither a failure nor a cost: the day's
// jev.shadow.failures does not move and nothing is stored, so a day of
// restarts cannot reach DailyFailureCap on its own.
func TestShadow_CloseCancellationIsNotCountedAsAFailure(t *testing.T) {
	e := holdCandidate(t)
	e.trader.Close() // returns once the cancelled call has finished
	cfg := jevshadowsettings.Load(context.Background(), e.settings)
	if got := cfg.Failures.FailuresOn("2026-10-22"); got != 0 {
		t.Fatalf("failure counter after Close = %d, want 0 (a cancelled call is not a failure)", got)
	}
	if n := e.count(`SELECT COUNT(*) FROM jev_shadow_decisions`); n != 0 {
		t.Fatalf("the cancelled call stored %d shadow rows", n)
	}
}
