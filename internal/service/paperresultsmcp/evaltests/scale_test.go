package evaltests

import (
	"database/sql"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/calibration"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/evaldiag"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

// resemblance of the 2026-11-11 Calibration screen: 1357 trader LONG/SHORT rows
// with a mean net return of about -8.1 bps. The real snapshot cannot be
// reproduced here (it is not in the repository, and this test never reads a real
// database): this is a synthetic fixture of the same size and mean whose labels
// are written by the production Labeler, and the test proves that the tool's
// headline number is the mean of those stored net_return_bps, row for row, with
// the decisions that passed Risk, were rejected and had no signal all counted.
const resemblanceRows = 1357

// elevenPattern is the LLM arm's net return (bps) of a repeating block of 20
// decisions: the default label gives +30 (take-profit), -30 (stop-loss), -20 and
// 0 (timeouts). The block sums to -160, a mean of -8.0 bps.
var elevenPattern = []int{30, -30, -20, 0, -30, -20, 0, -30, 30, -20, -30, 0, 0, 30, -30, -20, 0, -30, -20, 30}

// scenarioFor is the scenario that gives a LONG or SHORT the wanted net return.
func scenarioFor(direction string, net int) string {
	long := direction == domain.JevDirectionLong
	switch {
	case net == 30 && long, net == -30 && !long:
		return "up"
	case net == -30 && long, net == 30 && !long:
		return "down"
	case net == -20 && long, net == 0 && !long:
		return "dip"
	}
	return "mild" // LONG 0, SHORT -20
}

func TestElevenElevenLikeFixtureHeadlineEqualsTheStoredMean(t *testing.T) {
	srv, w := newServer(t)
	const instruments = 40
	x := newDB(t, w)
	for i := 3; i <= instruments; i++ {
		testkit.MustExec(t, w, `INSERT INTO instruments (id, symbol, name, market) VALUES (?, ?, 'x', 'TSE')`, i, fmt.Sprintf("S%03d", i))
	}

	var snaps []domain.Snapshot
	var decs []dec
	wantSum := 0.0
	for k := range resemblanceRows {
		net := elevenPattern[k%len(elevenPattern)]
		dir := domain.JevDirectionLong
		if k%2 == 1 {
			dir = domain.JevDirectionShort
		}
		d := dec{
			id: k + 1, day: 5 + k%5, slot: k / (5 * instruments), inst: k/5%instruments + 1, dir: dir, model: m1, version: v3,
			scenario: scenarioFor(dir, net), r5: new(float64(k%3 - 1)),
			signal: []string{"", "passed", "rejected"}[k%3],
		}
		decs = append(decs, d)
		snaps = append(snaps, barsOf(d, 13)...)
		wantSum += float64(net)
	}
	// one transaction for the decisions, one for the bars, one for the labels
	tx, err := w.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range decs {
		ts := testkit.Stored(t, d.at().Format(time.RFC3339))
		mustTx(t, tx, `INSERT INTO jev_decisions (id, instrument_id, symbol, timestamp, decision_type, state_hash, state_json,
			question_version, response_json, direction, confidence, latency_ms, model_id, created_at)
			VALUES (?, ?, 'S', ?, 'trader', 'h', '{}', ?, '{}', ?, 0.7, 5, ?, ?)`, d.id, d.inst, ts, d.version, d.dir, d.model, ts)
		if d.signal != "" {
			mustTx(t, tx, `INSERT INTO trade_signals (instrument_id, jev_decision_id, symbol, direction, timestamp, policy_version, risk_passed)
				VALUES (?, ?, 'S', ?, ?, 'policy-v2', ?)`, d.inst, d.id, d.dir, ts, d.signal == "passed")
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err := x.snapshots.InsertBatch(t.Context(), snaps); err != nil {
		t.Fatal(err)
	}
	labeler := calibration.NewLabeler(x.decisions, x.snapshots, x.outcomes)
	storedSum, stored := 0.0, 0
	var outcomes []domain.CalibrationOutcome
	for _, d := range decs { // the only connection is free while the labeler reads
		_, o, gap, err := labeler.Compute(t.Context(), int64(d.id), 10)
		if err != nil || gap != "" {
			t.Fatalf("label of %d: gap %q err %v", d.id, gap, err)
		}
		outcomes = append(outcomes, o)
		storedSum += *o.NetReturnBps
		stored++
	}
	tx, err = w.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range outcomes {
		mustTx(t, tx, `INSERT INTO calibration_outcomes (jev_decision_id, horizon_minutes, future_return, max_adverse_excursion,
			max_favorable_excursion, was_direction_correct, barrier_hit, net_return_bps, entry_ref_price, label_version)
			VALUES (?, 10, ?, ?, ?, ?, ?, ?, ?, 2)`, o.JevDecisionID, o.FutureReturn, o.MaxAdverseExcursion, o.MaxFavorableExcursion,
			*o.WasDirectionCorrect, o.BarrierHit, *o.NetReturnBps, *o.EntryRefPrice)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	wantMean := storedSum / float64(stored)
	if math.Abs(wantMean-wantSum/resemblanceRows) > 1e-9 || math.Abs(wantMean+8.1) > 0.3 {
		t.Fatalf("fixture mean = %v (pattern %v): not a -8.1 bps resemblance", wantMean, wantSum/resemblanceRows)
	}

	begin := time.Now()
	resp := srv.call(t, map[string]any{"from": "2026-10-05", "to": "2026-10-12"})
	t.Logf("jev_evaluation over %d decisions took %s", resemblanceRows, time.Since(begin))
	d := resp.data(t)
	if d.Decisions != resemblanceRows || len(d.Cohorts) != 1 {
		t.Fatalf("decisions %d, cohorts %d", d.Decisions, len(d.Cohorts))
	}
	c := d.Cohorts[0]
	llm := armOf(t, c, evaldiag.ArmLLM)
	if llm.All.N != resemblanceRows || llm.Long.N+llm.Short.N != resemblanceRows || c.Evaluated != resemblanceRows {
		t.Fatalf("evaluated %d, llm n %d (long %d short %d)", c.Evaluated, llm.All.N, llm.Long.N, llm.Short.N)
	}
	if got := *llm.All.MeanNetBps; math.Abs(got-wantMean) > 1e-9 {
		t.Fatalf("headline mean %v != mean of the stored net_return_bps %v", got, wantMean)
	}
	s := c.StoredLabel
	if s.AllDecisions.WithLabel != resemblanceRows || math.Abs(*s.AllDecisions.MeanNetBps-wantMean) > 1e-9 || *s.MaxAbsDifferenceBps > 1e-9 {
		t.Errorf("stored label = %+v max diff %v", s.AllDecisions, deref(s.MaxAbsDifferenceBps))
	}
	// Decisions without a passed signal (a third had none, a third were rejected) are counted: the entered
	// and skipped halves add up to every row (452 = the k%3==1 rows that carry a passed signal).
	entered, skipped := c.LLMByEntry[0].All, c.LLMByEntry[1].All
	if entered.N != 452 || entered.N+skipped.N != resemblanceRows {
		t.Errorf("entered %d + skipped %d, want %d rows", entered.N, skipped.N, resemblanceRows)
	}
	// NONE earns nothing, so the LLM's difference to it is its own (negative) mean.
	none := diffOf(t, c, evaldiag.ArmNone)
	if math.Abs(*none.All.MeanDiffBps-wantMean) > 1e-9 || none.All.N != resemblanceRows {
		t.Errorf("llm - none = %v", deref(none.All.MeanDiffBps))
	}
	if llm.All.Days != 5 {
		t.Errorf("days = %d", llm.All.Days)
	}
}

func mustTx(t *testing.T, tx *sql.Tx, q string, args ...any) {
	t.Helper()
	if _, err := tx.ExecContext(t.Context(), q, args...); err != nil {
		t.Fatalf("exec %q: %v", q[:30], err)
	}
}
