package exporttests

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/datasetexport"
)

// Without a split every trader decision of the period is one row of dataset.csv (NONE included,
// scouts and decisions outside the period not), oldest first; the file, its hash and the manifest
// agree, and the cells hold the values of the seeded rows.
func TestExportWithoutSplitIsOneRowPerTraderDecision(t *testing.T) {
	srv, w := newServer(t, "")
	decs := fixture()
	seed(t, w, decs)
	enableExport(t, w)

	resp := srv.call(t, nil).export(t)
	if !resp.Enabled || !resp.Written || resp.Reason != "" || resp.Manifest == nil {
		t.Fatalf("response %+v", resp)
	}
	m := resp.Manifest
	want := tradersInPeriod(decs)
	var inDB int
	if err := w.QueryRow(`SELECT COUNT(*) FROM jev_decisions WHERE decision_type = 'trader' AND timestamp >= '2026-10-04T15:00:00.000000000Z' AND timestamp < '2026-10-11T15:00:00.000000000Z'`).Scan(&inDB); err != nil {
		t.Fatal(err)
	}
	if inDB != len(want) || m.Decisions != len(want) || m.Rows != len(want) || m.RowsTrain != 0 || m.RowsEval != 0 || m.RowsInEmbargo != 0 || m.RowsPurged != 0 {
		t.Fatalf("decisions in the database %d, expected %d, manifest %+v", inDB, len(want), m)
	}
	if m.SplitAt != nil || m.TrainDays != nil || m.EvalDays != nil || m.EmbargoDays != 0 {
		t.Errorf("a split is reported without one: %+v", m)
	}

	dir := singleDataset(t, srv, m.DatasetID)
	files := datasetFiles(t, dir)
	if len(files) != 3 || files["dataset.csv"] == nil || files["dataset.parquet"] == nil || files["manifest.json"] == nil {
		t.Fatalf("files %v", slices.Collect(func(yield func(string) bool) {
			for k := range files {
				if !yield(k) {
					return
				}
			}
		}))
	}
	tb := parseCSV(t, files["dataset.csv"])
	if got := tb.ids(t); !slices.Equal(got, want) {
		t.Fatalf("rows = %v, want %v (timestamp, then id)", got, want)
	}
	if !slices.Equal(tb.header, datasetexport.ColumnNames()) || !slices.Equal(m.Columns, tb.header) {
		t.Errorf("header %v is not the whitelist", tb.header)
	}
	for i := range tb.rows {
		if tb.cell(t, i, "split") != "all" {
			t.Errorf("row %d split = %q, want all", i, tb.cell(t, i, "split"))
		}
	}

	// The hashes reproduce from the bytes on disk.
	if len(m.Files) != 2 || m.Files[0].Name != "dataset.csv" || m.Files[0].Format != "csv" || m.Files[1].Name != "dataset.parquet" || m.Files[1].Format != "parquet" {
		t.Fatalf("file entries %+v", m.Files)
	}
	for _, f := range m.Files {
		if f.SHA256 != sha(files[f.Name]) || f.Bytes != len(files[f.Name]) || f.Rows != len(want) {
			t.Errorf("file entry %+v", f)
		}
	}
	if overall := sha([]byte(specText("2", periodFrom, periodTo, "", 0) + "dataset.csv " + m.Files[0].SHA256 + "\ndataset.parquet " + m.Files[1].SHA256 + "\n")); m.DatasetSHA256 != overall || m.DatasetID != "ds-"+overall[:12] {
		t.Errorf("dataset hash %s id %s, want %s", m.DatasetSHA256, m.DatasetID, overall)
	}
	var onDisk datasetexport.Manifest
	if err := json.Unmarshal(files["manifest.json"], &onDisk); err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(onDisk)
	b, _ := json.Marshal(m)
	if string(a) != string(b) {
		t.Errorf("manifest.json differs from the response:\n%s\n%s", a, b)
	}
	if m.SaveDir != dir || m.SchemaVersion != "2" || m.CreatedAt != "2026-10-18T03:00:00Z" || m.From != periodFrom || m.To != periodTo {
		t.Errorf("manifest %+v", m)
	}
	assertModes(t, srv.exportsDir(), dir)
}

// assertModes: folders 0700, files 0600.
func assertModes(t *testing.T, dirs ...string) {
	t.Helper()
	for _, dir := range dirs {
		if info, err := os.Stat(dir); err != nil || info.Mode().Perm() != 0o700 {
			t.Errorf("%s: %v %v, want 0700", dir, info, err)
		}
	}
	entries, _ := os.ReadDir(dirs[len(dirs)-1])
	for _, e := range entries {
		if info, err := os.Stat(filepath.Join(dirs[len(dirs)-1], e.Name())); err != nil || info.Mode().Perm() != 0o600 {
			t.Errorf("%s: %v %v, want 0600", e.Name(), info, err)
		}
	}
}

// The cells are the values of the seeded rows: the answer, the derived bar features, the gate with
// a fixed reject code, every label source and the shadow and trial slots.
func TestCellsHoldTheSeededValues(t *testing.T) {
	srv, w := newServer(t, "")
	seed(t, w, fixture())
	enableExport(t, w)
	resp := srv.call(t, nil).export(t)
	tb := parseCSV(t, readFile(t, filepath.Join(resp.Manifest.SaveDir, "dataset.csv")))

	tb.wantText(t, 1, "decided_at", "2026-10-05T01:00:00.000000000Z")
	tb.wantText(t, 1, "decision_day", "2026-10-05")
	tb.wantText(t, 1, "symbol", "S")
	tb.wantText(t, 1, "question_version", "trader-v3")
	tb.wantText(t, 1, "answer_direction", "LONG")
	tb.wantText(t, 1, "answer_regime", "TREND")
	tb.wantText(t, 1, "answer_entry_quality", "good")
	tb.wantNum(t, 1, "answer_confidence", 0.6)
	tb.wantNum(t, 1, "answer_toxic_flow", 0.1)
	tb.wantNum(t, 1, "answer_continuation_probability", 0.7)
	tb.wantText(t, 1, "market_regime", "trend")
	tb.wantNum(t, 1, "regime_volatility_bps", 12.5)
	tb.wantNum(t, 1, "ofi_short", -40)
	tb.wantText(t, 1, "has_bar", "1")
	tb.wantNum(t, 1, "spread_bps", 2.5)
	tb.wantNum(t, 1, "return_5m", 0.004)
	tb.wantNum(t, 1, "price_vs_vwap_bps", 12.5)
	tb.wantNum(t, 1, "volume_ratio_5m", 1.5)
	tb.wantText(t, 1, "lendable", "1")
	tb.wantText(t, 1, "sector", "transport")
	tb.wantText(t, 13, "sector", "electric") // instrument 2

	// A decision with no bar has empty feature cells; one with an unknown entry_quality an empty answer.
	tb.wantText(t, 3, "has_bar", "0")
	tb.wantText(t, 3, "spread_bps", "")
	tb.wantText(t, 5, "answer_entry_quality", "")

	// The gate: a fixed code, never the free text.
	tb.wantText(t, 1, "origin", "entered")
	tb.wantText(t, 1, "risk_passed", "1")
	tb.wantText(t, 1, "reject_code", "")
	tb.wantText(t, 2, "origin", "risk_rejected")
	tb.wantText(t, 2, "reject_code", "risk_engine_rejected")
	tb.wantText(t, 4, "origin", "guard_skipped")
	tb.wantText(t, 4, "reject_code", "entry_quality_below_threshold")
	tb.wantText(t, 7, "reject_code", "other")
	tb.wantText(t, 5, "origin", "no_signal")
	tb.wantText(t, 5, "has_signal", "0")
	tb.wantText(t, 5, "signal_count", "0")
	tb.wantText(t, 3, "origin", "no_direction")

	// Labels: every horizon column, the research label per side, the counterfactual of a skipped candidate.
	for h, net := range map[string]float64{"5": 11, "10": 30, "15": 40, "30": 12, "60": -8} {
		tb.wantNum(t, 1, "calib_h"+h+"_net_bps", net)
	}
	tb.wantText(t, 1, "calib_h10_barrier", "timeout")
	tb.wantText(t, 4, "calib_h5_net_bps", "")
	tb.wantNum(t, 4, "calib_h10_net_bps", -5)
	tb.wantNum(t, 1, "rh_30m_long_net_bps", 10)
	tb.wantNum(t, 1, "rh_30m_short_net_bps", -30)
	tb.wantNum(t, 1, "rh_60m_long_net_bps", 20)
	tb.wantNum(t, 1, "rh_close_short_net_bps", -50)
	tb.wantText(t, 2, "rh_60m_long_net_bps", "")
	tb.wantText(t, 2, "cf_horizon_minutes", "10")
	tb.wantNum(t, 2, "cf_net_bps", 25.5)
	tb.wantText(t, 2, "cf_barrier", "tp")
	tb.wantText(t, 2, "cf_skip_kind", "risk_rejected")
	tb.wantText(t, 2, "cf_cutoff_unverified", "1")
	tb.wantText(t, 1, "cf_net_bps", "")
	tb.wantText(t, 5, "cf_skip_kind", "no_signal")

	// Shadow and trial answers fill the slots in the order of the manifest, the rest stay empty.
	if !slices.Equal(resp.Manifest.ShadowVersions, []string{"cand-a", "cand-b"}) || !slices.Equal(resp.Manifest.TrialArms, []string{"arm-x", "arm-y"}) {
		t.Errorf("slots: shadow %v trial %v", resp.Manifest.ShadowVersions, resp.Manifest.TrialArms)
	}
	tb.wantText(t, 1, "shadow1_version", "cand-a")
	tb.wantText(t, 1, "shadow1_direction", "SHORT")
	tb.wantNum(t, 1, "shadow1_confidence", 0.55)
	tb.wantText(t, 1, "shadow1_entry_quality", "fair")
	tb.wantText(t, 1, "shadow2_version", "cand-b")
	tb.wantText(t, 4, "shadow2_version", "")
	tb.wantText(t, 1, "shadow3_version", "")
	tb.wantText(t, 10, "trial1_arm_id", "arm-x")
	tb.wantText(t, 10, "trial2_arm_id", "arm-y")
	tb.wantText(t, 10, "trial1_status", "ok")
	tb.wantText(t, 10, "trial1_direction", "LONG")
	tb.wantNum(t, 10, "trial1_confidence", 0.8)
	tb.wantText(t, 10, "trial3_arm_id", "")

	// The stored label at a horizon with no column is counted, not exported.
	if resp.Manifest.LabelInfo.Calibration.LabelVersion != 2 || !slices.Equal(resp.Manifest.LabelInfo.Calibration.HorizonMinutes, []int{5, 10, 15, 30, 60}) ||
		resp.Manifest.LabelInfo.Research.LabelVersion != 1 || resp.Manifest.LabelInfo.Counterfactual.LabelVersion != 2 {
		t.Errorf("label info %+v", resp.Manifest.LabelInfo)
	}
}
