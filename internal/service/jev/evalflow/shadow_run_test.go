package evalflow_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/jevshadowsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/shadow"
)

func TestShadow_PaperEnabled_StoresALinkedRowForEveryLiveDecision(t *testing.T) {
	e := newShadowEnv(t, metalabelsettings.ModePaper, &fakeJev{})
	e.enable()

	first, second := e.evaluate(), e.evaluate()
	e.shadow.Wait()

	if first.QuestionVersion != "trader-v3" || second.QuestionVersion != "trader-v3" {
		t.Fatalf("the live decisions must stay on trader-v3, got %q / %q", first.QuestionVersion, second.QuestionVersion)
	}
	rows, err := e.db.Query(`SELECT s.jev_decision_id, s.prompt_version, s.entry_quality, s.direction, s.model_id, s.request_cost, s.input_tokens, s.response_json
		FROM jev_shadow_decisions s ORDER BY s.jev_decision_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var ids []int64
	for rows.Next() {
		var (
			id                    int64
			version, eq, dir, mdl string
			cost                  float64
			tokens                int
			resp                  string
		)
		if err := rows.Scan(&id, &version, &eq, &dir, &mdl, &cost, &tokens, &resp); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
		if version != "trader-v4" || eq != "good" || dir != "LONG" || mdl != "jev-test" || tokens != 1200 || !strings.Contains(resp, `"entry_quality":"good"`) {
			t.Errorf("shadow row = %s %s %s %s tokens=%d resp=%s", version, eq, dir, mdl, tokens, resp)
		}
		if want := shadow.InputCostUSD(1200); cost != want || cost <= 0 {
			t.Errorf("request_cost = %v, want the priced input tokens %v", cost, want)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || ids[0] != first.ID || ids[1] != second.ID {
		t.Fatalf("shadow rows link to live decisions %v, want [%d %d]", ids, first.ID, second.ID)
	}
	if e.api.calls("trader-v3") != 2 || e.api.calls("trader-v4") != 2 {
		t.Errorf("calls v3=%d v4=%d, want 2 each", e.api.calls("trader-v3"), e.api.calls("trader-v4"))
	}
}

func TestShadow_IsOffByDefault(t *testing.T) {
	e := newShadowEnv(t, metalabelsettings.ModePaper, &fakeJev{})
	e.evaluate()
	e.shadow.Wait()
	if n := e.api.calls("trader-v4"); n != 0 {
		t.Fatalf("a candidate call was made while the shadow is off by default: %d", n)
	}
	// Enabled but with no candidate chosen: still nothing.
	e.set(jevshadowsettings.KeyEnabled, "1")
	e.evaluate()
	e.shadow.Wait()
	if n := e.count(`SELECT COUNT(*) FROM jev_shadow_decisions`); n != 0 || e.api.calls("trader-v4") != 0 {
		t.Fatalf("shadow rows=%d candidate calls=%d, want 0 without a candidate", n, e.api.calls("trader-v4"))
	}
	// A damaged switch is off, never on.
	e.set(jevshadowsettings.KeyEnabled, `"yes"`)
	e.set(jevshadowsettings.KeyCandidateVersion, `"trader-v4"`)
	e.evaluate()
	e.shadow.Wait()
	if e.api.calls("trader-v4") != 0 {
		t.Fatal("a damaged switch value turned the shadow on")
	}
}

func TestShadow_DoesNotRunInLiveMode(t *testing.T) {
	e := newShadowEnv(t, metalabelsettings.ModeLive, &fakeJev{})
	e.enable()
	live := e.evaluate()
	e.shadow.Wait()
	if live.ID == 0 {
		t.Fatal("the live decision must still be made in Live mode")
	}
	if n := e.api.calls("trader-v4"); n != 0 {
		t.Fatalf("the shadow called Jev in Live mode: %d calls", n)
	}
	if n := e.count(`SELECT COUNT(*) FROM jev_shadow_decisions`); n != 0 {
		t.Fatalf("%d shadow rows in Live mode", n)
	}
}

func TestShadow_ShadowRowsNeverEnterRAGOrJevDecisions(t *testing.T) {
	e := newShadowEnv(t, metalabelsettings.ModePaper, &fakeJev{})
	e.enable()
	e.evaluate()
	e.shadow.Wait()
	if n := e.count(`SELECT COUNT(*) FROM jev_shadow_decisions`); n != 1 {
		t.Fatalf("shadow rows = %d, want 1", n)
	}
	// Only the live decision is a jev_decisions row and is indexed in the RAG vectors.
	if n := e.count(`SELECT COUNT(*) FROM jev_decisions`); n != 1 {
		t.Errorf("jev_decisions rows = %d, want 1 (the live one)", n)
	}
	if n := e.count(`SELECT COUNT(*) FROM jev_decisions WHERE question_version = 'trader-v4'`); n != 0 {
		t.Errorf("%d jev_decisions rows carry the candidate version", n)
	}
	if n := e.count(`SELECT COUNT(*) FROM jev_decision_vectors`); n != 1 {
		t.Errorf("jev_decision_vectors rows = %d, want 1 (the live decision only)", n)
	}
	for _, table := range []string{"trade_signals", "calibration_outcomes", "paper_orders", "positions"} {
		if n := e.count(`SELECT COUNT(*) FROM ` + table); n != 0 {
			t.Errorf("%s has %d rows after a shadow comparison, want 0", table, n)
		}
	}
}

func TestShadow_FailureAndSlownessNeverAffectTheLiveDecision(t *testing.T) {
	hold := make(chan struct{})
	e := newShadowEnv(t, metalabelsettings.ModePaper, &fakeJev{failShadow: true, holdShadow: hold})
	e.enable()

	done := make(chan domain.JevDecision, 1)
	go func() { done <- e.evaluate() }()
	select {
	case live := <-done:
		if live.ID == 0 || live.EntryQuality == nil || *live.EntryQuality != domain.JevEntryQualityPoor {
			t.Fatalf("live decision = %+v", live)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the live decision waited for the shadow call")
	}
	close(hold) // now let the candidate call fail with a 500
	e.shadow.Wait()
	if n := e.count(`SELECT COUNT(*) FROM jev_shadow_decisions`); n != 0 {
		t.Fatalf("a failed shadow call stored %d rows", n)
	}
	if n := e.count(`SELECT COUNT(*) FROM jev_decisions`); n != 1 {
		t.Fatalf("jev_decisions rows = %d, want the live one", n)
	}
}

// A failing candidate must not count towards the jev_api_down error rate the
// Kill Switch reads: 12 live successes each paired with a failed shadow call
// would be a 50% failure rate over 24 calls if the shadow counted.
func TestShadow_FailuresDoNotFeedTheJevAPIErrorRate(t *testing.T) {
	e := newShadowEnv(t, metalabelsettings.ModePaper, &fakeJev{failShadow: true})
	e.enable()
	for range 12 {
		e.evaluate()
		e.shadow.Wait()
	}
	if e.api.calls("trader-v4") != 12 {
		t.Fatalf("candidate calls = %d, want 12 failing ones", e.api.calls("trader-v4"))
	}
	if ok, err := e.client.Healthy(context.Background()); err != nil || !ok {
		t.Fatalf("Healthy = %v, %v: failing shadow calls tripped the jev_api_down signal", ok, err)
	}
}

func TestShadow_DailyCostCapStopsTheShadowAndLiveContinues(t *testing.T) {
	e := newShadowEnv(t, metalabelsettings.ModePaper, &fakeJev{})
	e.enable()
	perCall := shadow.InputCostUSD(1200)
	// Room for exactly two calls: the check runs before each call, so spend
	// after the second reaches the cap and the third is not made.
	e.set(jevshadowsettings.KeyDailyCostCapUSD, jsonNumber(perCall*2))

	for range 4 {
		if live := e.evaluate(); live.ID == 0 {
			t.Fatal("the live decision stopped")
		}
		e.shadow.Wait() // serialize so each cap check sees the previous call's cost
	}
	if n := e.count(`SELECT COUNT(*) FROM jev_shadow_decisions`); n != 2 {
		t.Fatalf("shadow rows = %d, want 2 (stopped at the cap)", n)
	}
	if e.api.calls("trader-v4") != 2 || e.api.calls("trader-v3") != 4 {
		t.Fatalf("calls v3=%d v4=%d, want 4 and 2", e.api.calls("trader-v3"), e.api.calls("trader-v4"))
	}
	cfg := jevshadowsettings.Load(context.Background(), e.settings)
	if cfg.Halt == nil || cfg.Halt.Reason != jevshadowsettings.HaltReasonDailyCostCap || cfg.Halt.Date != "2026-10-22" || cfg.Halt.SpentUSD < perCall*2-1e-12 {
		t.Fatalf("halt record = %+v, want the daily_cost_cap stop of 2026-10-22", cfg.Halt)
	}

	// The next JST day the shadow runs again.
	e.now = e.now.Add(24 * time.Hour)
	e.evaluate()
	e.shadow.Wait()
	if n := e.count(`SELECT COUNT(*) FROM jev_shadow_decisions`); n != 3 {
		t.Fatalf("shadow rows the next day = %d, want 3", n)
	}
}

// The cost cap sums stored calls only, so a candidate that answers invalidly
// every time (a call the vendor may bill but nothing stores) would run forever
// at a recorded cost of 0. Failed calls count too: the shadow stops for the day
// at ProposedDailyFailureCap of them, the reason is recorded, the live decision
// goes on, and the next JST day starts a fresh count.
func TestShadow_InvalidResponsesStopTheShadowAtTheFailureCapAndLiveContinues(t *testing.T) {
	e := newShadowEnv(t, metalabelsettings.ModePaper, &fakeJev{invalidShadow: true})
	e.enable()
	capN := jevshadowsettings.ProposedDailyFailureCap
	for range capN + 5 {
		if live := e.evaluate(); live.ID == 0 {
			t.Fatal("the live decision stopped")
		}
		e.shadow.Wait()
	}
	if got := e.api.calls("trader-v4"); got != capN {
		t.Fatalf("candidate calls = %d, want %d (stopped at the failure cap)", got, capN)
	}
	if got := e.api.calls("trader-v3"); got != capN+5 {
		t.Fatalf("live calls = %d, want %d", got, capN+5)
	}
	if n := e.count(`SELECT COUNT(*) FROM jev_shadow_decisions`); n != 0 {
		t.Fatalf("invalid responses stored %d shadow rows", n)
	}
	cfg := jevshadowsettings.Load(context.Background(), e.settings)
	if cfg.Halt == nil || cfg.Halt.Reason != jevshadowsettings.HaltReasonDailyFailureCap || cfg.Halt.Date != "2026-10-22" ||
		cfg.Halt.Failures != capN || cfg.Halt.FailureCap != capN {
		t.Fatalf("halt record = %+v, want the daily_failure_cap stop of 2026-10-22 at %d failures", cfg.Halt, capN)
	}
	if got := cfg.Failures.FailuresOn("2026-10-22"); got != capN {
		t.Fatalf("failure counter = %d, want %d", got, capN)
	}

	e.now = e.now.Add(24 * time.Hour) // the next JST day: a fresh count
	e.evaluate()
	e.shadow.Wait()
	if got := e.api.calls("trader-v4"); got != capN+1 {
		t.Fatalf("candidate calls the next day = %d, want %d", got, capN+1)
	}
}

// Close (the application's shutdown) cancels the calls in flight instead of
// waiting out their timeouts, and comparisons submitted afterwards are dropped.
func TestShadow_CloseCancelsCallsInFlightAndDropsLaterOnes(t *testing.T) {
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
