package calibration_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/barriersettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	calrepo "github.com/ousiassllc/pitha-trador/internal/repository/calibration"
	"github.com/ousiassllc/pitha-trador/internal/repository/jobqueue"
	"github.com/ousiassllc/pitha-trador/internal/service/calibration"
)

// Default barriers (barriersettings.Default): TP 30 bps, SL 30 bps, cost 10
// bps, so the gross move that touches the take-profit is +40 bps and the one
// that touches the stop-loss is -20 bps.

// LONG is priced at the ask: an ask 1000 / bid 998 entry, then a bar whose
// last price is +45 bps above the entry touches the take-profit; the later
// bars never matter.
func TestLabeler_HandleJob_LongTakeProfitMeasuredFromAsk(t *testing.T) {
	f := newLabelerFixtures(t)
	base := time.Date(2026, 9, 27, 9, 0, 0, 0, time.UTC)
	decision := f.insertDecision(t, base, domain.JevDirectionLong, 0.82)
	f.insertBars(t, base, map[time.Duration]bar{
		0:               {price: 999, bid: 998, ask: 1000}, // entry: LONG pays the ask
		1 * time.Minute: {price: 1002, bid: 1001, ask: 1003},
		2 * time.Minute: {price: 1004.5, bid: 1004, ask: 1005}, // +45 bps vs ask 1000: take-profit
		3 * time.Minute: {price: 900, bid: 899, ask: 901},      // would be a stop-loss, but TP came first
		5 * time.Minute: {price: 1000, bid: 999, ask: 1001},
	})

	if err := f.handleJob(t, decision.ID, 5); err != nil {
		t.Fatalf("HandleJob: %v", err)
	}

	o := f.getTripleBarrier(t, decision.ID, 5)
	if o.barrierHit != domain.BarrierHitTakeProfit || o.labelVersion != domain.CalibrationLabelVersionTripleBarrier {
		t.Fatalf("barrier_hit/label_version = %q/%d, want tp/%d", o.barrierHit, o.labelVersion, domain.CalibrationLabelVersionTripleBarrier)
	}
	if !almostEqual(o.entryRefPrice, 1000) {
		t.Fatalf("entry_ref_price = %v, want the ask 1000", o.entryRefPrice)
	}
	if !almostEqual(o.netReturnBps, 30) {
		t.Fatalf("net_return_bps = %v, want +30 (take-profit width)", o.netReturnBps)
	}
	if !o.correct.Valid || !o.correct.Bool {
		t.Fatalf("was_direction_correct = %v, want true", o.correct)
	}
	if !almostEqual(o.futureReturn, 0.45) {
		t.Fatalf("future_return = %v, want 0.45 (entry to the bar that ended the label)", o.futureReturn)
	}
	mfe, mae := f.excursions(t, decision.ID, 5)
	if !almostEqual(mfe, 0.45) || !almostEqual(mae, 0) {
		t.Fatalf("MFE/MAE = %v/%v, want 0.45/0 (excursions stop at the touched barrier)", mfe, mae)
	}
}

func (f labelerFixtures) excursions(t *testing.T, decisionID int64, horizonMinutes int) (mfe, mae float64) {
	t.Helper()
	if err := f.db.QueryRow(
		`SELECT max_favorable_excursion, max_adverse_excursion FROM calibration_outcomes WHERE jev_decision_id = ? AND horizon_minutes = ?`,
		decisionID, horizonMinutes,
	).Scan(&mfe, &mae); err != nil {
		t.Fatalf("query excursions: %v", err)
	}
	return mfe, mae
}

// SHORT is priced at the bid and a rising price is adverse: bid 1000, then
// a bar +25 bps above it is past the -20 bps gross stop-loss level.
func TestLabeler_HandleJob_ShortStopLossMeasuredFromBid(t *testing.T) {
	f := newLabelerFixtures(t)
	base := time.Date(2026, 9, 27, 9, 0, 0, 0, time.UTC)
	decision := f.insertDecision(t, base, domain.JevDirectionShort, 0.7)
	f.insertBars(t, base, map[time.Duration]bar{
		0:               {price: 1001, bid: 1000, ask: 1002}, // entry: SHORT sells at the bid
		1 * time.Minute: {price: 1002.5, bid: 1002, ask: 1003},
		5 * time.Minute: {price: 990, bid: 989, ask: 991}, // recovered, but the stop-loss came first
	})

	if err := f.handleJob(t, decision.ID, 5); err != nil {
		t.Fatalf("HandleJob: %v", err)
	}

	o := f.getTripleBarrier(t, decision.ID, 5)
	if o.barrierHit != domain.BarrierHitStopLoss {
		t.Fatalf("barrier_hit = %q, want sl", o.barrierHit)
	}
	if !almostEqual(o.entryRefPrice, 1000) {
		t.Fatalf("entry_ref_price = %v, want the bid 1000", o.entryRefPrice)
	}
	if !almostEqual(o.netReturnBps, -30) {
		t.Fatalf("net_return_bps = %v, want -30 (stop-loss width)", o.netReturnBps)
	}
	if !o.correct.Valid || o.correct.Bool {
		t.Fatalf("was_direction_correct = %v, want false", o.correct)
	}
	if !almostEqual(o.futureReturn, 0.25) {
		t.Fatalf("future_return = %v, want 0.25 (raw, direction-neutral)", o.futureReturn)
	}
	mfe, mae := f.excursions(t, decision.ID, 5)
	if !almostEqual(mae, -0.25) || !almostEqual(mfe, 0) {
		t.Fatalf("MFE/MAE = %v/%v, want 0/-0.25", mfe, mae)
	}
}

// No barrier touched: the label times out and is graded by its net return,
// so a small gain that the cost eats up is wrong (not "flat = wrong" but
// "net <= 0 = wrong") and a gain above the cost is right.
func TestLabeler_HandleJob_TimeoutGradedByNetReturn(t *testing.T) {
	for name, tc := range map[string]struct {
		lastPrice   float64
		wantNet     float64
		wantCorrect bool
	}{
		"gain above the cost":  {lastPrice: 1003, wantNet: 20, wantCorrect: true},    // +30 bps - 10 bps
		"gain eaten by cost":   {lastPrice: 1000.5, wantNet: -5, wantCorrect: false}, // +5 bps - 10 bps
		"flat is a cost loss":  {lastPrice: 1000, wantNet: -10, wantCorrect: false},
		"loss within barriers": {lastPrice: 999, wantNet: -20, wantCorrect: false}, // -10 bps - 10 bps
	} {
		t.Run(name, func(t *testing.T) {
			f := newLabelerFixtures(t)
			base := time.Date(2026, 9, 27, 9, 0, 0, 0, time.UTC)
			decision := f.insertDecision(t, base, domain.JevDirectionLong, 0.82)
			f.insertBars(t, base, map[time.Duration]bar{
				0:               {price: 1000, bid: 999.5, ask: 1000},
				2 * time.Minute: {price: 1001, bid: 1000.5, ask: 1001},
				5 * time.Minute: {price: tc.lastPrice, bid: tc.lastPrice - 0.5, ask: tc.lastPrice},
			})

			if err := f.handleJob(t, decision.ID, 5); err != nil {
				t.Fatalf("HandleJob: %v", err)
			}

			o := f.getTripleBarrier(t, decision.ID, 5)
			if o.barrierHit != domain.BarrierHitTimeout {
				t.Fatalf("barrier_hit = %q, want timeout", o.barrierHit)
			}
			if !almostEqual(o.netReturnBps, tc.wantNet) {
				t.Fatalf("net_return_bps = %v, want %v", o.netReturnBps, tc.wantNet)
			}
			if !o.correct.Valid || o.correct.Bool != tc.wantCorrect {
				t.Fatalf("was_direction_correct = %v, want %v", o.correct, tc.wantCorrect)
			}
		})
	}
}

// The reference price is the first executable quote at or after the Jev
// response: a bar stamped before the response (even the decision's own bar)
// is not an entry, and a response later than the decision's bar moves the
// entry with it.
func TestLabeler_HandleJob_EntryIsFirstQuoteAfterJevResponse(t *testing.T) {
	f := newLabelerFixtures(t)
	base := time.Date(2026, 9, 27, 9, 0, 0, 0, time.UTC)
	decision, err := f.decisions.Insert(context.Background(), domain.JevDecision{
		InstrumentID: f.instrument.ID, Symbol: f.instrument.Symbol, Timestamp: base,
		CreatedAt:    base.Add(70 * time.Second), // Jev answered 70 s after the decision's bar
		DecisionType: domain.JevDecisionTypeTrader, StateHash: "hash", StateJSON: "{}",
		QuestionVersion: "v1", ResponseJSON: "{}", Direction: ptr(domain.JevDirectionLong),
		Confidence: ptr(0.8), ModelID: "test-model",
	})
	if err != nil {
		t.Fatalf("insert decision: %v", err)
	}
	f.insertBars(t, base, map[time.Duration]bar{
		0:                              {price: 1000, bid: 999, ask: 1000},  // before the response
		1 * time.Minute:                {price: 1000, bid: 999, ask: 1000},  // before the response
		2 * time.Minute:                {price: 1010, bid: 1009, ask: 1010}, // first bar after: entry
		2*time.Minute + 30*time.Second: {price: 1011, bid: 1010, ask: 1011},
		7 * time.Minute:                {price: 1012, bid: 1011, ask: 1012},
	})

	if err := f.handleJob(t, decision.ID, 5); err != nil {
		t.Fatalf("HandleJob: %v", err)
	}

	o := f.getTripleBarrier(t, decision.ID, 5)
	if !almostEqual(o.entryRefPrice, 1010) {
		t.Fatalf("entry_ref_price = %v, want 1010 (the first ask after the response)", o.entryRefPrice)
	}
}

// A bar without the side's quote is not an entry: LONG needs an ask, SHORT
// a bid. The next bar that has one is.
func TestLabeler_HandleJob_BarWithoutQuoteIsNotAnEntry(t *testing.T) {
	f := newLabelerFixtures(t)
	base := time.Date(2026, 9, 27, 9, 0, 0, 0, time.UTC)
	decision := f.insertDecision(t, base, domain.JevDirectionLong, 0.82)
	f.insertBars(t, base, map[time.Duration]bar{
		0:               {price: 1000, bid: 999}, // no ask
		1 * time.Minute: {price: 1000, bid: 999, ask: 1001},
		6 * time.Minute: {price: 1000, bid: 999, ask: 1001},
	})

	if err := f.handleJob(t, decision.ID, 5); err != nil {
		t.Fatalf("HandleJob: %v", err)
	}

	if o := f.getTripleBarrier(t, decision.ID, 5); !almostEqual(o.entryRefPrice, 1001) {
		t.Fatalf("entry_ref_price = %v, want 1001 (the first bar with an ask)", o.entryRefPrice)
	}
}

// The barrier widths come from Settings on every job.
func TestLabeler_HandleJob_UsesBarrierSettings(t *testing.T) {
	f := newLabelerFixtures(t)
	store := barrierStore{
		barriersettings.KeyTakeProfitBps: "10", barriersettings.KeyStopLossBps: "50", barriersettings.KeyCostBps: "0",
	}
	labeler := calibration.NewLabeler(f.decisions, f.snapshots, f.outcomes,
		calibration.WithClock(func() time.Time { return *f.now }), calibration.WithBarrierSettings(store))
	base := time.Date(2026, 9, 27, 9, 0, 0, 0, time.UTC)
	decision := f.insertDecision(t, base, domain.JevDirectionLong, 0.82)
	f.insertBars(t, base, map[time.Duration]bar{
		0:               {price: 1000, bid: 999, ask: 1000},
		1 * time.Minute: {price: 1001.5, bid: 1001, ask: 1002}, // +15 bps: past the 10 bps TP, short of the default 40
		5 * time.Minute: {price: 1000, bid: 999, ask: 1000},
	})
	payload, _ := json.Marshal(calrepo.OutcomeLabelJobPayload{JevDecisionID: decision.ID, HorizonMinutes: 5})
	if err := labeler.HandleJob(context.Background(), jobqueue.Job{PayloadJSON: string(payload)}); err != nil {
		t.Fatalf("HandleJob: %v", err)
	}

	o := f.getTripleBarrier(t, decision.ID, 5)
	if o.barrierHit != domain.BarrierHitTakeProfit || !almostEqual(o.netReturnBps, 10) {
		t.Fatalf("barrier_hit/net_return_bps = %q/%v, want tp/10 with the Settings widths", o.barrierHit, o.netReturnBps)
	}
}

type barrierStore map[string]string

func (s barrierStore) Get(_ context.Context, key string) (string, bool, error) {
	v, ok := s[key]
	return v, ok, nil
}

// A NONE decision has no side to price: the raw price path is recorded with
// no barrier fields and no direction verdict.
func TestLabeler_HandleJob_NoneDirectionHasNoBarrierFields(t *testing.T) {
	f := newLabelerFixtures(t)
	base := time.Date(2026, 9, 27, 9, 0, 0, 0, time.UTC)
	decision, err := f.decisions.Insert(context.Background(), domain.JevDecision{
		InstrumentID: f.instrument.ID, Symbol: f.instrument.Symbol, Timestamp: base, CreatedAt: base,
		DecisionType: domain.JevDecisionTypeTrader, StateHash: "hash", StateJSON: "{}",
		QuestionVersion: "v1", ResponseJSON: "{}", Direction: ptr(domain.JevDirectionNone),
		ModelID: "test-model",
	})
	if err != nil {
		t.Fatalf("insert NONE decision: %v", err)
	}
	f.insertBars(t, base, map[time.Duration]bar{
		0:               {price: 1000},
		5 * time.Minute: {price: 1010},
	})

	if err := f.handleJob(t, decision.ID, 5); err != nil {
		t.Fatalf("HandleJob: %v", err)
	}

	o := f.getTripleBarrier(t, decision.ID, 5)
	if o.correct.Valid {
		t.Fatalf("was_direction_correct = %v, want NULL for direction=NONE", o.correct)
	}
	if o.barrierHit != "" || o.entryRefPrice != 0 || o.netReturnBps != 0 {
		t.Fatalf("barrier fields = %+v, want none for direction=NONE", o)
	}
	if !almostEqual(o.futureReturn, 1.0) {
		t.Fatalf("future_return = %v, want 1.0", o.futureReturn)
	}
}
