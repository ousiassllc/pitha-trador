package evaltests

import (
	"database/sql"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	calrepo "github.com/ousiassllc/pitha-trador/internal/repository/calibration"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/service/calibration"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

// Scenario price paths, one bar per minute from the decision's bar (offset 0).
// With the default label (TP 30, SL 30, cost 10 bps, 10 minutes: a take-profit
// at a gross move of +40 bps, a stop-loss at -20 bps) they give these net
// returns (bps) for a LONG / SHORT:
//
//	up    1000, 1000, then 1005   LONG +30 (TP)    SHORT -30 (SL)
//	down  1000, 1000, then 995    LONG -30 (SL)    SHORT +30 (TP)
//	mild  1000, then 1001         LONG   0 (timeout, +10 gross - 10)   SHORT -20 (timeout)
//	dip   1000, then 999          LONG -20 (timeout)                   SHORT   0 (timeout)
func scenarioPrices(name string, n int) []float64 {
	prices := make([]float64, n)
	for i := range prices {
		prices[i] = 1000
	}
	tail := map[string]struct {
		from  int
		price float64
	}{"up": {2, 1005}, "down": {2, 995}, "mild": {1, 1001}, "dip": {1, 999}}[name]
	for i := tail.from; i < n; i++ {
		prices[i] = tail.price
	}
	return prices
}

// dec is one synthetic decision with its bars.
type dec struct {
	id             int
	day, slot      int // 2026-10-<day> 00:00Z (09:00 JST) + slot*20 minutes
	inst           int
	dir            string
	model, version string
	scenario       string
	r5             *float64 // return_5m of the decision's bar
	signal         string   // "" none, "passed", "rejected", "rejected2" (two rejected signals)
	kind           string   // decision_type; "" is trader
	decAt, barAt   time.Duration
	bars           int  // bars from barAt; 0 = 13 (offsets 0..12)
	noAsk          bool // no ask on any bar
	noBidFirst     int  // the first n bars have no bid
}

func (d dec) at() time.Time {
	return time.Date(2026, 10, d.day, 0, 0, 0, 0, time.UTC).Add(time.Duration(d.slot) * 20 * time.Minute)
}

// db wires the repositories the fixture writes with. The stored labels are made
// by the production Labeler (Compute + CalibrationRepository.Insert), never by
// the code under test.
type db struct {
	w         *sql.DB
	decisions *judgement.DecisionRepository
	snapshots *market.SnapshotRepository
	outcomes  *calrepo.CalibrationRepository
}

func newDB(t *testing.T, w *sql.DB) db {
	t.Helper()
	testkit.MustExec(t, w, `INSERT INTO instruments (id, symbol, name, market) VALUES (1, '7203', 'a', 'TSE'), (2, '6758', 'b', 'TSE')`)
	return db{w: w, decisions: judgement.NewDecisionRepository(w), snapshots: market.NewSnapshotRepository(w), outcomes: calrepo.NewCalibrationRepository(w)}
}

// insert writes the decision, its bars and signals; label stores the Labeler's
// own outcome at horizon 10 (the Settings timeout).
func (x db) insert(t *testing.T, d dec, label bool) {
	t.Helper()
	kind := d.kind
	if kind == "" {
		kind = "trader"
	}
	ts := d.at().Add(d.decAt)
	testkit.MustExec(t, x.w, `INSERT INTO jev_decisions (id, instrument_id, symbol, timestamp, decision_type, state_hash, state_json,
		question_version, response_json, direction, confidence, latency_ms, model_id, created_at)
		VALUES (?, ?, 'S', ?, ?, 'h', '{}', ?, '{}', ?, 0.7, 5, ?, ?)`,
		d.id, d.inst, testkit.Stored(t, ts.Format(time.RFC3339)), kind, d.version, d.dir, d.model, testkit.Stored(t, ts.Format(time.RFC3339)))
	n := d.bars
	if n == 0 {
		n = 13
	}
	if d.scenario != "" {
		x.insertBars(t, d, n)
	}
	switch d.signal {
	case "passed":
		x.signal(t, d, true)
	case "rejected":
		x.signal(t, d, false)
	case "rejected2":
		x.signal(t, d, false)
		x.signal(t, d, false)
	}
	if label && kind == "trader" && d.dir != domain.JevDirectionNone {
		_, o, gap, err := calibration.NewLabeler(x.decisions, x.snapshots, x.outcomes).Compute(t.Context(), int64(d.id), 10)
		if err != nil {
			t.Fatal(err)
		}
		if gap == "" {
			if _, err := x.outcomes.Insert(t.Context(), o); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func (x db) insertBars(t *testing.T, d dec, n int) {
	t.Helper()
	if _, err := x.snapshots.InsertBatch(t.Context(), barsOf(d, n)); err != nil {
		t.Fatal(err)
	}
}

// barsOf is the n one-minute bars of the decision's scenario.
func barsOf(d dec, n int) []domain.Snapshot {
	var snaps []domain.Snapshot
	for i, price := range scenarioPrices(d.scenario, n) {
		s := domain.Snapshot{
			InstrumentID: int64(d.inst), Symbol: "S", Timestamp: d.at().Add(d.barAt + time.Duration(i)*time.Minute), Price: price,
		}
		if !d.noAsk {
			s.Ask = new(price)
		}
		if i >= d.noBidFirst {
			s.Bid = new(price)
		}
		if i == 0 {
			s.Feature.Return5m = d.r5
		}
		snaps = append(snaps, s)
	}
	return snaps
}

func (x db) signal(t *testing.T, d dec, passed bool) {
	t.Helper()
	var reason any
	if !passed {
		reason = "entry_quality_below_min"
	}
	testkit.MustExec(t, x.w, `INSERT INTO trade_signals (instrument_id, jev_decision_id, symbol, direction, timestamp, policy_version, risk_passed, reject_reason)
		VALUES (?, ?, 'S', ?, ?, 'policy-v2', ?, ?)`, d.inst, d.id, d.dir, testkit.Stored(t, d.at().Format(time.RFC3339)), passed, reason)
}

// The cohort constants of the fixture.
const (
	m1, v3 = "model-1", "trader-v3"
	m2, v4 = "model-2", "trader-v4"
)

// small is the hand-computed dataset (period 2026-10-05..2026-10-09 JST).
//
// Cohort model-1/trader-v3, compared (ids 1-6; legs L/S, nets of the Scenario table above):
//
//	id day dir   scenario  return_5m signal    llm  long short momentum contrarian
//	1  5   LONG  up        +1        passed    +30  +30  -30   +30(L)   -30(S)
//	2  5   SHORT up        +1        none      -30  +30  -30   +30(L)   -30(S)
//	3  6   LONG  down      -1        rejected  -30  -30  +30   +30(S)   -30(L)
//	4  6   SHORT down      -1        passed    +30  -30  +30   +30(S)   -30(L)
//	5  7   LONG  mild      0         rejected    0    0  -20     0        0
//	6  7   SHORT up        NULL      none      -30  +30  -30     0        0
//
// Excluded in that cohort: 9 (LONG, no ask: llm_no_entry_quote), 10 (LONG, data
// ends at +3 min: llm_window_short), 11 (SHORT, no ask: the LONG leg has no entry
// quote), 12 (LONG, no bar at its own timestamp: no_bar_for_momentum), 13 (LONG,
// no bid on the first three bars, data ends at +10: the SHORT leg's window is
// short). Cohort model-2/trader-v4: ids 7, 8, 14 (14 is on instrument 2, whose
// bars differ from instrument 1's at the same minute).
func small() []dec {
	r := func(v float64) *float64 { return &v }
	return []dec{
		{id: 1, day: 5, slot: 0, inst: 1, dir: "LONG", model: m1, version: v3, scenario: "up", r5: r(1), signal: "passed"},
		{id: 2, day: 5, slot: 1, inst: 1, dir: "SHORT", model: m1, version: v3, scenario: "up", r5: r(1), signal: "rejected2"},
		{id: 12, day: 5, slot: 2, inst: 1, dir: "LONG", model: m1, version: v3, scenario: "mild", decAt: 30 * time.Second, barAt: time.Minute, signal: "passed"},
		{id: 3, day: 6, slot: 0, inst: 1, dir: "LONG", model: m1, version: v3, scenario: "down", r5: r(-1), signal: "rejected"},
		{id: 4, day: 6, slot: 1, inst: 1, dir: "SHORT", model: m1, version: v3, scenario: "down", r5: r(-1), signal: "passed"},
		{id: 10, day: 6, slot: 2, inst: 1, dir: "LONG", model: m1, version: v3, scenario: "up", r5: r(1), bars: 4},
		{id: 11, day: 6, slot: 3, inst: 1, dir: "SHORT", model: m1, version: v3, scenario: "up", r5: r(1), noAsk: true},
		{id: 13, day: 6, slot: 4, inst: 1, dir: "LONG", model: m1, version: v3, scenario: "up", r5: r(1), bars: 11, noBidFirst: 3},
		{id: 15, day: 6, slot: 5, inst: 1, dir: "NONE", model: m1, version: v3, scenario: "up", r5: r(1)},
		{id: 16, day: 6, slot: 6, inst: 1, dir: "LONG", model: m1, version: v3, scenario: "up", r5: r(1), kind: "scout"},
		{id: 5, day: 7, slot: 0, inst: 1, dir: "LONG", model: m1, version: v3, scenario: "mild", r5: r(0), signal: "rejected"},
		{id: 6, day: 7, slot: 1, inst: 1, dir: "SHORT", model: m1, version: v3, scenario: "up"},
		{id: 9, day: 7, slot: 2, inst: 1, dir: "LONG", model: m1, version: v3, scenario: "up", r5: r(1), noAsk: true},
		{id: 7, day: 7, slot: 3, inst: 1, dir: "LONG", model: m2, version: v4, scenario: "up", r5: r(-2), signal: "passed"},
		{id: 8, day: 8, slot: 0, inst: 1, dir: "SHORT", model: m2, version: v4, scenario: "mild", r5: r(1), signal: "rejected"},
		// the same minute on two instruments: only the decision's own bars may count
		{id: 14, day: 8, slot: 1, inst: 2, dir: "SHORT", model: m2, version: v4, scenario: "down", r5: r(-1)},
		{id: 17, day: 2, slot: 0, inst: 1, dir: "LONG", model: m1, version: v3, scenario: "up", r5: r(1)}, // before the period
	}
}

// seedSmall writes small() (and the conflicting instrument-1 bars of id 14).
func seedSmall(t *testing.T, w *sql.DB) db {
	t.Helper()
	x := newDB(t, w)
	for _, d := range small() {
		x.insert(t, d, true)
	}
	x.insertBars(t, dec{inst: 1, day: 8, slot: 1, scenario: "up"}, 13)
	return x
}
