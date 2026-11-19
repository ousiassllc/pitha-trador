package exporttests

import (
	"database/sql"
	"net/http"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/datasetexport"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

// seedMany writes n trader decisions spread over 2026-10-05 .. 2026-10-10 (JST, 09:00 onward) on 50
// instruments, each with its own bar, a trade signal, a stored label at 10 minutes, the three research
// labels, a shadow answer and a trial answer: every source the export joins, at the size of the limit.
func seedMany(t *testing.T, w *sql.DB, n int) {
	t.Helper()
	testkit.MustExec(t, w, `WITH RECURSIVE seq(n) AS (SELECT 1 UNION ALL SELECT n + 1 FROM seq WHERE n < 50)
		INSERT INTO instruments (id, symbol, name, market, sector) SELECT n, printf('%04d', n), 'x', 'TSE', 'sector' FROM seq`)
	const ts = `strftime('%Y-%m-%dT%H:%M:%f000000Z', 1791158400 + (n % 6) * 86400 + (n / 6) * 5, 'unixepoch')` // 2026-10-05 00:00Z = 09:00 JST
	with := `WITH RECURSIVE seq(n) AS (SELECT 1 UNION ALL SELECT n + 1 FROM seq WHERE n < ` + strconv.Itoa(n) + `) `
	for _, stmt := range []string{
		`INSERT INTO jev_decisions (id, instrument_id, symbol, timestamp, decision_type, state_hash, state_json, question_version, response_json, direction, confidence, latency_ms, model_id, created_at)
			SELECT n, n % 50 + 1, 'S', ` + ts + `, 'trader', 'h', '{}', 'trader-v4',
			'{"direction":"LONG","regime":"TREND","entry_quality":"good","toxic_flow":0.1,"liquidity_stressed":0.2,"continuation_probability":0.7,"confidence":0.65}',
			CASE n % 3 WHEN 0 THEN 'LONG' WHEN 1 THEN 'SHORT' ELSE 'NONE' END, 0.6, 5, 'model-1', ` + ts + ` FROM seq`,
		`INSERT INTO market_snapshots (instrument_id, symbol, timestamp, price, bid, ask, spread_bps, volume, turnover, return_5m, vwap, price_vs_vwap_bps, volume_ratio_5m, raw_data_json)
			SELECT n % 50 + 1, 'S', ` + ts + `, 1000, 999, 1001, 2.5, 100, 100000, 0.004, 1000, 1.5, 1.2, '{}' FROM seq`,
		`INSERT INTO trade_signals (instrument_id, jev_decision_id, symbol, direction, timestamp, policy_version, risk_passed, reject_reason)
			SELECT n % 50 + 1, n, 'S', 'LONG', ` + ts + `, 'p', n % 2, CASE n % 2 WHEN 0 THEN 'spread_too_wide: 12 bps' END FROM seq`,
		`INSERT INTO calibration_outcomes (jev_decision_id, horizon_minutes, future_return, max_adverse_excursion, max_favorable_excursion, label_version, net_return_bps, barrier_hit, entry_ref_price)
			SELECT n, 10, 0.1, -0.1, 0.2, 2, 5.5, 'tp', 1000 FROM seq`,
		`INSERT INTO research_horizon_labels (snapshot_id, instrument_id, symbol, snapshot_at, day, sample_bucket, horizon, exit_at, entry_price, exit_price, long_gross_bps, long_cost_bps, long_net_bps, label_version)
			SELECT n * 3 + h.k, n % 50 + 1, 'S', ` + ts + `, '2026-10-05', 1, h.name, ` + ts + `, 1000, 1001, 10, 10, 0, 1
			FROM seq, (SELECT 0 AS k, '30m' AS name UNION ALL SELECT 1, '60m' UNION ALL SELECT 2, 'close') h`,
		`INSERT INTO jev_shadow_decisions (jev_decision_id, prompt_version, model_id, response_json, direction, regime, entry_quality, confidence, toxic_flow, liquidity_stressed, continuation_probability, latency_ms)
			SELECT n, 'cand-a', 'm', '{}', 'SHORT', 'RANGE', 'fair', 0.5, 0.1, 0.1, 0.5, 5 FROM seq`,
		`INSERT INTO jev_trial_decisions (jev_decision_id, arm_id, prompt_version, requested_model, status, direction, regime, entry_quality, confidence, toxic_flow, liquidity_stressed, continuation_probability, response_json, cost_source)
			SELECT n, 'arm-x', 'trader-v4', 'req', 'ok', 'LONG', 'TREND', 'strong', 0.8, 0.1, 0.1, 0.9, '{}', 'unknown' FROM seq`,
	} {
		testkit.MustExec(t, w, with+stmt)
	}
}

// The decision limit: MaxDecisions decisions export whole (every source joined, one row each, hash
// verifies), one more is limit_exceeded and writes nothing. The full-size export is timed and logged
// (about 1.3 s on a developer machine, with the Parquet files, against the production 3 s budget); the server here runs with a
// longer budget so a loaded CI runner does not turn the timing into a failure. Skipped under -short and
// the race detector.
func TestTheDecisionLimitIsEnforcedAndTheFullSizeExports(t *testing.T) {
	if testing.Short() || raceEnabled {
		t.Skip("seeds the full limit; the race detector slows the SQLite driver too much for the full size")
	}
	srv, w := newServer(t, "")
	seedMany(t, w, datasetexport.MaxDecisions)
	enableExport(t, w)

	start := time.Now()
	resp := srv.call(t, map[string]any{"to": "2026-10-11", "split_at": "2026-10-08"}).export(t)
	elapsed := time.Since(start)
	t.Logf("exporting %d decisions took %v", datasetexport.MaxDecisions, elapsed)
	for _, f := range resp.Manifest.Files {
		t.Logf("%s: %d rows, %d bytes", f.Name, f.Rows, f.Bytes)
	}
	m := resp.Manifest
	if m == nil || m.Decisions != datasetexport.MaxDecisions || m.RowsTrain+m.RowsEval+m.RowsInEmbargo+m.RowsPurged != datasetexport.MaxDecisions {
		t.Fatalf("manifest %+v", m)
	}
	for _, f := range m.Files {
		if sha(readFile(t, filepath.Join(m.SaveDir, f.Name))) != f.SHA256 {
			t.Errorf("%s does not hash to the manifest", f.Name)
		}
	}
	sameAsCSV(t, m.SaveDir, m.Files[0], m.Files[1]) // 10001 rows: more than one row group
	sameAsCSV(t, m.SaveDir, m.Files[2], m.Files[3])
	tb := parseCSV(t, readFile(t, filepath.Join(m.SaveDir, "dataset_eval.csv")))
	row := tb.rowOf(t, 1+datasetexport.MaxDecisions/2)
	if tb.cell(t, row, "calib_h10_net_bps") != "5.5" || tb.cell(t, row, "rh_close_long_net_bps") != "0" || tb.cell(t, row, "shadow1_version") != "cand-a" || tb.cell(t, row, "trial1_arm_id") != "arm-x" {
		t.Errorf("a joined source is missing in the middle of the dataset: %v", tb.rows[row])
	}

	testkit.MustExec(t, w, `INSERT INTO jev_decisions (id, instrument_id, symbol, timestamp, decision_type, state_hash, state_json, question_version, response_json, direction, confidence, latency_ms, model_id, created_at)
		VALUES (900000, 1, 'S', '2026-10-10T00:00:00.000000000Z', 'trader', 'h', '{}', 'v', '{}', 'NONE', 0.5, 5, 'm', '2026-10-10T00:00:00.000000000Z')`)
	entriesBefore := listDir(t, srv.dbDir)
	srv.call(t, map[string]any{"to": "2026-10-11"}).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeLimitExceeded)
	if after := listDir(t, srv.dbDir); len(after) != len(entriesBefore) {
		t.Errorf("a refused export wrote something: %v", after)
	}
}
