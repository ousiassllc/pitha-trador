package exporttests

import (
	"cmp"
	"database/sql"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

// Raw values planted where the export must never read them. Each is a number or text that
// would show in a file if the column were exported.
const (
	rawBid       = "11111.1111"
	rawAsk       = "22222.2222"
	rawBidDepth  = "33333.3333"
	rawMicro     = "44444.4444"
	rawVWAP      = "55555.5555"
	rawTurnover  = "66666.6666"
	rawPrice     = "98765.4321"
	sentinelFree = "SENTINEL_FREE_TEXT_REASON"
	sentinelKey  = "SENTINEL_API_KEY_VALUE"
	sentinelErr  = "SENTINEL_ERROR_EXCERPT"
	sentinelName = "SENTINEL_INSTRUMENT_NAME"
)

// seedDec is one synthetic decision with what hangs off it.
type seedDec struct {
	id, inst  int
	day       int // 2026-10-<day> JST
	hh, mm    int
	dir       string // LONG / SHORT / NONE
	version   string
	kind      string // decision_type; "" is trader
	bar       bool
	signal    string          // "", "passed", "risk", "guard", "weird"
	calib     map[int]float64 // horizon (minutes) -> net bps of the stored label (version 2)
	research  []string        // horizons with a research label
	cf        string          // "" or the skip_kind of a counterfactual label
	shadow    []string        // shadow prompt versions
	trial     []string        // trial arms
	badAnswer bool            // the answer's entry_quality is not one of the five
}

func (d seedDec) at() time.Time { return time.Date(2026, 10, d.day, d.hh, d.mm, 0, 0, query.JST).UTC() }

func (d seedDec) inPeriod() bool {
	from := time.Date(2026, 10, 5, 0, 0, 0, 0, query.JST)
	to := time.Date(2026, 10, 12, 0, 0, 0, 0, query.JST)
	return d.kind == "" && !d.at().Before(from) && d.at().Before(to)
}

// fixture is the dataset the tests export (period 2026-10-05 .. 2026-10-12).
//
//	day 5  1 LONG entered, full labels, shadow a+b, trial x      2 SHORT risk-rejected, counterfactual   3 NONE, no bar
//	day 6  4 LONG guard-skipped (free-text reason)               5 SHORT no signal, counterfactual
//	day 7  6 LONG entered                                        7 LONG at 23:50 JST with a 30-minute label   8 scout (never exported)
//	day 8  9 LONG (10:01)  10 SHORT (10:00: seen before 9)          day 9  11, 12      day 10  13 (instrument 2)      day 11  14, 17 (23:59 JST)
//	outside: 15 (day 4), 16 (2026-10-12 00:00 JST, the excluded end of the period)
func fixture() []seedDec {
	all := map[int]float64{5: 11, 10: 30, 15: 40, 30: 12, 60: -8}
	return []seedDec{
		{id: 1, inst: 1, day: 5, hh: 10, dir: "LONG", version: "trader-v3", bar: true, signal: "passed", calib: all, research: []string{"30m", "60m", "close"}, shadow: []string{"cand-a", "cand-b"}, trial: []string{"arm-x"}},
		{id: 2, inst: 1, day: 5, hh: 10, mm: 5, dir: "SHORT", version: "trader-v3", bar: true, signal: "risk", cf: "risk_rejected", research: []string{"30m"}},
		{id: 3, inst: 1, day: 5, hh: 10, mm: 10, dir: "NONE", version: "trader-v3"},
		{id: 4, inst: 1, day: 6, hh: 10, dir: "LONG", version: "trader-v3", bar: true, signal: "guard", calib: map[int]float64{10: -5}, shadow: []string{"cand-a"}},
		{id: 5, inst: 1, day: 6, hh: 10, mm: 5, dir: "SHORT", version: "trader-v3", bar: true, cf: "no_signal", badAnswer: true},
		{id: 6, inst: 1, day: 7, hh: 10, dir: "LONG", version: "trader-v3", bar: true, signal: "passed", calib: map[int]float64{10: 7}, research: []string{"close"}},
		{id: 7, inst: 1, day: 7, hh: 23, mm: 50, dir: "LONG", version: "trader-v3", signal: "weird", calib: map[int]float64{30: 3}},
		{id: 8, inst: 1, day: 7, hh: 11, dir: "LONG", version: "trader-v3", kind: "scout", bar: true},
		{id: 9, inst: 1, day: 8, hh: 10, mm: 1, dir: "LONG", version: "trader-v4", bar: true, signal: "passed", calib: map[int]float64{10: 1}},
		{id: 10, inst: 1, day: 8, hh: 10, dir: "SHORT", version: "trader-v4", bar: true, trial: []string{"arm-x", "arm-y"}},
		{id: 11, inst: 1, day: 9, hh: 10, dir: "LONG", version: "trader-v4", bar: true, signal: "passed", calib: map[int]float64{10: 2}},
		{id: 12, inst: 1, day: 9, hh: 10, mm: 1, dir: "NONE", version: "trader-v4", bar: true},
		{id: 13, inst: 2, day: 10, hh: 10, dir: "SHORT", version: "trader-v4", bar: true, signal: "guard"},
		{id: 14, inst: 1, day: 11, hh: 10, dir: "LONG", version: "trader-v4", bar: true, signal: "passed", calib: map[int]float64{10: -3}},
		{id: 17, inst: 1, day: 11, hh: 23, mm: 59, dir: "LONG", version: "trader-v4"},
		{id: 15, inst: 1, day: 4, hh: 10, dir: "LONG", version: "trader-v3", bar: true},
		{id: 16, inst: 1, day: 12, dir: "LONG", version: "trader-v4"},
	}
}

// seed writes decs, with the raw columns the export must never expose filled with the sentinels above.
func seed(t *testing.T, w *sql.DB, decs []seedDec) {
	t.Helper()
	testkit.MustExec(t, w, `INSERT INTO instruments (id, symbol, name, market, sector) VALUES (1, '7203', ?, 'TSE', 'transport'), (2, '6758', ?, 'TSE', 'electric')`, sentinelName, sentinelName)
	// Secrets and other settings live in runtime_settings: none of it may reach a file.
	testkit.MustExec(t, w, `INSERT INTO runtime_settings (key, value, updated_at) VALUES ('jquants_api_key', ?, 't'), ('policy.long.min_confidence', '"`+sentinelKey+`"', 't')`, sentinelKey)
	for _, d := range decs {
		seedOne(t, w, d)
	}
}

func seedOne(t *testing.T, w *sql.DB, d seedDec) {
	t.Helper()
	ts := testkit.Stored(t, d.at().Format(time.RFC3339))
	kind := d.kind
	if kind == "" {
		kind = "trader"
	}
	quality := "good"
	if d.badAnswer {
		quality = testkit.SentinelResponse // not one of the five values: must come out empty
	}
	response := fmt.Sprintf(`{"direction":%q,"regime":"TREND","entry_quality":%q,"toxic_flow":0.1,"liquidity_stressed":0.2,"continuation_probability":0.7,"confidence":0.65,"note":%q}`,
		d.dir, quality, testkit.SentinelResponse)
	testkit.MustExec(t, w, `INSERT INTO jev_decisions (id, instrument_id, symbol, timestamp, decision_type, state_hash, state_json, question_version,
		response_json, direction, confidence, latency_ms, model_id, created_at, data_age_ms, market_regime, regime_volatility_bps, ofi_short)
		VALUES (?, ?, 'S', ?, ?, 'h', ?, ?, ?, ?, 0.6, 5, 'model-1', ?, 250, 'trend', 12.5, -40)`,
		d.id, d.inst, ts, kind, `{"s":"`+testkit.SentinelState+`"}`, d.version, response, d.dir, ts)
	if d.bar {
		testkit.MustExec(t, w, `INSERT INTO market_snapshots (instrument_id, symbol, timestamp, price, bid, ask, spread_bps, volume, turnover, return_5m,
			vwap, price_vs_vwap_bps, volume_ratio_5m, raw_data_json, bid_depth, ask_depth, microprice, special_quote, price_limit, lendable)
			VALUES (?, 'S', ?, `+rawPrice+`, `+rawBid+`, `+rawAsk+`, 2.5, 1000, `+rawTurnover+`, 0.004, `+rawVWAP+`, 12.5, 1.5, ?, `+rawBidDepth+`, `+rawBidDepth+`, `+rawMicro+`, 0, '', 1)`,
			d.inst, ts, `{"raw":"`+testkit.SentinelRaw+`"}`)
	}
	seedGate(t, w, d, ts)
	for h, net := range d.calib {
		testkit.MustExec(t, w, `INSERT INTO calibration_outcomes (jev_decision_id, horizon_minutes, future_return, max_adverse_excursion, max_favorable_excursion,
			label_version, net_return_bps, barrier_hit, entry_ref_price) VALUES (?, ?, 0.1, -0.1, 0.2, 2, ?, 'timeout', 1000)`, d.id, h, net)
	}
	for i, h := range d.research {
		net := float64(10 * (i + 1))
		testkit.MustExec(t, w, `INSERT INTO research_horizon_labels (snapshot_id, instrument_id, symbol, snapshot_at, day, sample_bucket, horizon, exit_at, entry_price, exit_price,
			long_gross_bps, long_cost_bps, long_net_bps, short_gross_bps, short_cost_bps, short_net_bps, label_version)
			VALUES (?, ?, 'S', ?, ?, 1, ?, ?, 1000, 1001, ?, 10, ?, ?, 10, ?, 1)`,
			d.id*10+i, d.inst, ts, d.at().In(query.JST).Format(time.DateOnly), h, ts, net+10, net, -net-10, -net-20)
	}
	if d.cf != "" {
		testkit.MustExec(t, w, `INSERT INTO counterfactual_labels (jev_decision_id, horizon_minutes, reject_reason, barrier_hit, net_return_bps, entry_ref_price,
			label_version, future_return, max_adverse_excursion, max_favorable_excursion, llm_cutoff, skip_kind, cutoff_unverified)
			VALUES (?, 10, ?, 'tp', 25.5, 1000, 2, 0.3, -0.1, 0.4, '2026-01-01', ?, 1)`, d.id, sentinelFree, d.cf)
	}
	for _, v := range d.shadow {
		testkit.MustExec(t, w, `INSERT INTO jev_shadow_decisions (jev_decision_id, prompt_version, model_id, response_json, direction, regime, entry_quality, confidence,
			toxic_flow, liquidity_stressed, continuation_probability, latency_ms) VALUES (?, ?, 'm', ?, 'SHORT', 'RANGE', 'fair', 0.55, 0.1, 0.1, 0.5, 5)`,
			d.id, v, `{"s":"`+testkit.SentinelResponse+`"}`)
	}
	for _, arm := range d.trial {
		testkit.MustExec(t, w, `INSERT INTO jev_trial_decisions (jev_decision_id, arm_id, prompt_version, requested_model, status, failure_code, error_excerpt, direction, regime,
			entry_quality, confidence, toxic_flow, liquidity_stressed, continuation_probability, response_json, cost_source)
			VALUES (?, ?, 'trader-v3', 'req', 'ok', '', ?, 'LONG', 'TREND', 'strong', 0.8, 0.1, 0.1, 0.9, ?, 'unknown')`,
			d.id, arm, sentinelErr, `{"s":"`+testkit.SentinelResponse+`"}`)
	}
}

// seedGate writes the decision's trade signal: the reject reasons are free text with a sentinel.
func seedGate(t *testing.T, w *sql.DB, d seedDec, ts string) {
	t.Helper()
	passed, reason := 0, ""
	switch d.signal {
	case "":
		return
	case "passed":
		passed = 1
	case "risk":
		reason = "risk_engine_rejected: " + sentinelFree
	case "guard":
		reason = "entry_quality_below_threshold: " + sentinelFree + " (0.31 < 0.50)"
	case "weird":
		reason = "something_new: " + sentinelFree
	}
	testkit.MustExec(t, w, `INSERT INTO trade_signals (instrument_id, jev_decision_id, symbol, direction, timestamp, policy_version, risk_passed, reject_reason)
		VALUES (?, ?, 'S', 'LONG', ?, 'policy-v2', ?, NULLIF(?, ''))`, d.inst, d.id, ts, passed, reason)
}

// tradersInPeriod are the ids of the fixture's trader decisions of the period, by the test's own rule.
// in file order: the decision time, then the id.
func tradersInPeriod(decs []seedDec) []int {
	var in []seedDec
	for _, d := range decs {
		if d.inPeriod() {
			in = append(in, d)
		}
	}
	slices.SortFunc(in, func(a, b seedDec) int { return cmp.Or(a.at().Compare(b.at()), cmp.Compare(a.id, b.id)) })
	ids := make([]int, len(in))
	for i, d := range in {
		ids[i] = d.id
	}
	return ids
}
