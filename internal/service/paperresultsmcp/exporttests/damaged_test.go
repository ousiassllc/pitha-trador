package exporttests

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

// A damaged row never fails the export or leaks: a response_json that is not JSON gives empty answer cells,
// and text sitting in a numeric column (of the bar, of a stored label, of a shadow answer) is written as empty.
func TestDamagedValuesComeOutEmptyAndNeverLeak(t *testing.T) {
	srv, w := newServer(t, "")
	seed(t, w, fixture())
	enableExport(t, w)
	ts := testkit.Stored(t, "2026-10-09T05:00:00Z")
	testkit.MustExec(t, w, `INSERT INTO jev_decisions (id, instrument_id, symbol, timestamp, decision_type, state_hash, state_json, question_version, response_json, direction, confidence, latency_ms, model_id, created_at, data_age_ms)
		VALUES (100, 1, 'S', ?, 'trader', 'h', '{}', 'trader-v4', 'not json: SENTINEL_RESPONSE_JSON', 'LONG', 'SENTINEL_STATE_JSON', 5, 'model-1', ?, 'SENTINEL_FEATURES_JSON')`, ts, ts)
	testkit.MustExec(t, w, `INSERT INTO market_snapshots (instrument_id, symbol, timestamp, price, volume, turnover, vwap, price_vs_vwap_bps, raw_data_json, return_5m, spread_bps, price_limit)
		VALUES (1, 'S', ?, 1, 1, 1, 1, 1, '{}', 'SENTINEL_RAW_DATA_JSON', 'SENTINEL_PAYLOAD_JSON', '')`, ts)
	testkit.MustExec(t, w, `INSERT INTO calibration_outcomes (jev_decision_id, horizon_minutes, future_return, max_adverse_excursion, max_favorable_excursion, label_version, net_return_bps, barrier_hit)
		VALUES (100, 10, 0, 0, 0, 2, 'SENTINEL_LAST_ERROR', NULL)`)

	r := srv.call(t, nil)
	resp := r.export(t)
	tb := parseCSV(t, readFile(t, filepath.Join(resp.Manifest.SaveDir, "dataset.csv")))
	row := tb.rowOf(t, 100)
	for _, col := range []string{"answer_regime", "answer_entry_quality", "answer_toxic_flow", "answer_confidence", "data_age_ms", "return_5m", "spread_bps", "calib_h10_net_bps", "calib_h10_barrier"} {
		if got := tb.cell(t, row, col); got != "" {
			t.Errorf("%s = %q, want empty", col, got)
		}
	}
	tb.wantText(t, 100, "answer_direction", "LONG") // the direction column is a checked enum, and valid
	tb.wantText(t, 100, "has_bar", "1")
	for _, s := range forbidden() {
		if bytes.Contains(readFile(t, filepath.Join(resp.Manifest.SaveDir, "dataset.csv")), []byte(s)) || bytes.Contains(r.body, []byte(s)) {
			t.Errorf("%q leaked", s)
		}
	}
}

// Rebuilding gives the same bytes and the same id independent of what is on disk: remove the folder
// and export again.
func TestRebuildAfterDeletingTheFolderGivesIdenticalBytes(t *testing.T) {
	srv, w := newServer(t, "")
	seed(t, w, fixture())
	enableExport(t, w)
	first := srv.call(t, map[string]any{"split_at": "2026-10-09", "embargo_days": 1}).export(t)
	want := datasetFiles(t, first.Manifest.SaveDir)
	if err := os.RemoveAll(srv.exportsDir()); err != nil {
		t.Fatal(err)
	}
	second := srv.call(t, map[string]any{"split_at": "2026-10-09", "embargo_days": 1}).export(t)
	if !second.Written || second.Manifest.DatasetID != first.Manifest.DatasetID {
		t.Fatalf("second %+v", second)
	}
	for name, data := range datasetFiles(t, second.Manifest.SaveDir) {
		if !bytes.Equal(data, want[name]) {
			t.Errorf("%s differs after a rebuild", name)
		}
	}
}
