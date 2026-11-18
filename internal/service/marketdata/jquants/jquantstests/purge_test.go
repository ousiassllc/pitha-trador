package jquantstests

import (
	"context"
	"testing"

	jqservice "github.com/ousiassllc/pitha-trador/internal/service/marketdata/jquants"
	"github.com/ousiassllc/pitha-trador/internal/service/researchlabel/jquantsrelabel"
)

// seedOtherSources gives every table the purge must not touch some rows.
func seedOtherSources(t *testing.T, r *rig) map[string]int {
	t.Helper()
	r.seedSnapshot(t, "2026-11-16T05:00:00.000000000Z", 3000, 2999, 3001)
	r.exec(t, `INSERT INTO daily_bars (symbol, trade_date, open, high, low, close, volume, adj_open, adj_high, adj_low, adj_close, adj_volume)
		VALUES ('7203', '2026-11-16', 1, 1, 1, 1, 1, 1, 1, 1, 1, 1)`)
	r.exec(t, `INSERT INTO disclosure_items (symbol, source_id, published_at, title, first_seen_at) VALUES ('7203', 's1', 't', 'title', 't')`)
	r.exec(t, `INSERT INTO jev_decisions (id, instrument_id, symbol, timestamp, decision_type, state_hash, state_json, question_version, response_json, latency_ms, model_id)
		VALUES (1, 1, '7203', 't', 'trader', 'h', '{}', 'q', '{}', 1, 'm')`)
	// A research label the recorder wrote (exit from a market snapshot) and one the
	// J-Quants re-labeling wrote.
	const label = `INSERT INTO research_horizon_labels (snapshot_id, instrument_id, symbol, snapshot_at, day, sample_bucket, horizon, exit_at, entry_price, exit_price,
		label_version, long_gross_bps, long_cost_bps, long_net_bps, exit_source) VALUES (?, 1, '7203', 't', '2026-11-16', 7, ?, 't', 1, 1, 1, 1.0, 1.0, 0.0, ?)`
	r.exec(t, label, 1, "60m", "snapshot")
	r.exec(t, label, 1, "30m", "jquants")
	r.exec(t, label, 2, "30m", "jquants")
	r.exec(t, `INSERT INTO research_label_days (day, snapshots, labeled_30m, labeled_60m, labeled_close, completed_at) VALUES ('2026-11-16', 1, 2, 1, 0, 't')`)
	return map[string]int{"market_snapshots": 1, "daily_bars": 1, "disclosure_items": 1, "jev_decisions": 1, "instruments": 1, "research_label_days": 1}
}

// The purge removes every J-Quants-sourced row — the five tables' data, the coverage
// marks and the research labels whose exit came from J-Quants — and nothing else:
// the application's own snapshots, daily bars, disclosures, decisions and the
// recorder's labels survive. The request ledger (counts and fixed codes) is kept so
// the day's cap does not reset.
func TestPurge_RemovesOnlyJQuantsRows(t *testing.T) {
	r := newRig(t, testKey, settingsFor(true, true, 100))
	others := seedOtherSources(t, r)
	if _, err := r.backfiller(0).Run(context.Background(), jqservice.Request{}); err != nil {
		t.Fatal(err)
	}
	if r.count(t, "research_jquants_daily_bars") == 0 || r.count(t, "research_jquants_minute_bars") == 0 || r.count(t, "research_jquants_coverage") == 0 {
		t.Fatal("setup: nothing was fetched")
	}
	ledgerBefore := r.count(t, "research_jquants_fetch_log")
	requests, _ := r.repo.RequestsOn(context.Background(), "2026-11-17")

	relabeler := jquantsrelabel.New(r.labels, r.repo, nil)
	res, err := jqservice.Purge(context.Background(), r.repo, relabeler)
	if err != nil {
		t.Fatalf("Purge: %v", err)
	}
	if res.DerivedLabels != 2 || res.Tables["research_jquants_daily_bars"] != 3 || res.Tables["research_jquants_minute_bars"] != 3 {
		t.Errorf("purge result = %+v, want 2 derived labels, 3 daily bars, 3 minute bars", res)
	}

	for _, table := range []string{"research_jquants_symbols", "research_jquants_daily_bars", "research_jquants_minute_bars", "research_jquants_coverage"} {
		if n := r.count(t, table); n != 0 {
			t.Errorf("%s has %d rows after the purge", table, n)
		}
	}
	var n int
	if err := r.db.QueryRow(`SELECT COUNT(*) FROM research_horizon_labels WHERE exit_source = 'jquants'`).Scan(&n); err != nil || n != 0 {
		t.Errorf("J-Quants-derived labels left: %d (%v)", n, err)
	}
	if err := r.db.QueryRow(`SELECT COUNT(*) FROM research_horizon_labels WHERE exit_source = 'snapshot'`).Scan(&n); err != nil || n != 1 {
		t.Errorf("the recorder's labels = %d (%v), want the 1 kept", n, err)
	}
	for table, want := range others {
		if got := r.count(t, table); got != want {
			t.Errorf("%s has %d rows after the purge, want %d (not J-Quants data)", table, got, want)
		}
	}
	if got := r.count(t, "research_jquants_fetch_log"); got != ledgerBefore {
		t.Errorf("fetch log rows %d -> %d: the ledger must survive", ledgerBefore, got)
	}
	if got, _ := r.repo.RequestsOn(context.Background(), "2026-11-17"); got != requests {
		t.Errorf("the day's request count %d -> %d: a purge must not reset the cap", requests, got)
	}
	// The sums of the day no longer count the deleted labels.
	var l30 int
	if err := r.db.QueryRow(`SELECT labeled_30m FROM research_label_days WHERE day = '2026-11-16'`).Scan(&l30); err != nil || l30 != 0 {
		t.Errorf("labeled_30m of the day = %d (%v), want 0 after the J-Quants labels are gone", l30, err)
	}
	if reqs := r.server.count(); reqs != 4 {
		t.Errorf("%d requests in total, want the 4 of the backfill and none from the purge", reqs)
	}

	// Run again over an empty store: nothing to do, no error.
	if res, err := jqservice.Purge(context.Background(), r.repo, relabeler); err != nil || res.DerivedLabels != 0 {
		t.Errorf("second Purge = (%+v, %v)", res, err)
	}
}
