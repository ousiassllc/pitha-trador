package exporttests

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/datasetexport"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

// forbidden is everything planted in a column the export must not read, as it would appear in a file.
func forbidden() []string {
	return append(testkit.Sentinels(), sentinelFree, sentinelKey, sentinelErr, sentinelName, rawBid, rawAsk, rawBidDepth, rawMicro, rawVWAP, rawTurnover, rawPrice,
		"jquants_api_key", "policy.long.min_confidence", testAPIKey)
}

// No file and no response contains a secret or a raw body: the sentinels seeded into response_json,
// state_json, raw_data_json, runtime_settings, the free-text reject reasons, error excerpts, the board
// and quote columns and the price levels appear nowhere; the header is the whitelist exactly with no
// excluded column in it; and the response is the manifest alone (no row).
func TestNoSecretNoRawBodyAndNoRowDataLeaves(t *testing.T) {
	srv, w := newServer(t, "")
	seed(t, w, fixture())
	enableExport(t, w)
	for _, args := range []map[string]any{nil, {"split_at": "2026-10-08", "embargo_days": 1}} {
		r := srv.call(t, args)
		resp := r.export(t)
		for _, s := range forbidden() {
			if bytes.Contains(r.body, []byte(s)) {
				t.Errorf("the response contains %q: %s", s, r.body)
			}
		}
		// The response carries no row: its keys are the response's and the manifest's, nothing else.
		assertShape(t, r)
		for name, data := range datasetFiles(t, resp.Manifest.SaveDir) {
			for _, s := range forbidden() {
				if bytes.Contains(data, []byte(s)) {
					t.Errorf("%s contains %q", name, s)
				}
			}
			if !strings.HasSuffix(name, ".csv") {
				continue
			}
			tb := parseCSV(t, data)
			if !slices.Equal(tb.header, datasetexport.ColumnNames()) {
				t.Errorf("%s header is not the whitelist: %v", name, tb.header)
			}
			for _, h := range tb.header {
				if slices.Contains(datasetexport.ExcludedColumns, h) {
					t.Errorf("%s has the excluded column %s", name, h)
				}
			}
		}
		if !slices.Equal(resp.Manifest.ExcludedColumns, datasetexport.ExcludedColumns) || len(resp.Manifest.ExcludedColumns) == 0 {
			t.Errorf("excluded columns %v", resp.Manifest.ExcludedColumns)
		}
		if err := os.RemoveAll(srv.exportsDir()); err != nil {
			t.Fatal(err)
		}
	}
}

// Off is the default: with no setting, a damaged value, another job's switch on or the switch at 0, the
// call says so and the filesystem is untouched: not even the exports folder is created. On writes.
func TestOffByDefaultWritesNothingAndOnWrites(t *testing.T) {
	srv, w := newServer(t, "")
	seed(t, w, fixture())
	before := listDir(t, srv.dbDir)

	wantOff := func(label string) {
		t.Helper()
		resp := srv.call(t, nil).export(t)
		if resp.Enabled || resp.Written || resp.Reason != datasetexport.ReasonDisabled || resp.Manifest != nil || resp.Notice == "" {
			t.Errorf("%s: %+v", label, resp)
		}
		if after := listDir(t, srv.dbDir); !slices.Equal(after, before) {
			t.Errorf("%s: the folder changed: %v -> %v", label, before, after)
		}
	}
	wantOff("nothing stored")
	for label, value := range map[string]string{"zero": "0", "damaged": `"yes"`, "two": "2", "null": "null", "float": "0.5"} {
		testkit.MustExec(t, w, `INSERT OR REPLACE INTO runtime_settings (key, value, updated_at) VALUES ('research.export.enabled', ?, 't')`, value)
		wantOff(label)
	}
	testkit.MustExec(t, w, `DELETE FROM runtime_settings WHERE key = 'research.export.enabled'`)
	testkit.MustExec(t, w, `INSERT INTO runtime_settings (key, value, updated_at) VALUES ('research.eval.enabled', '1', 't'), ('research.model.enabled', '1', 't')`)
	wantOff("the other research switches on")

	enableExport(t, w)
	resp := srv.call(t, nil).export(t)
	if !resp.Enabled || !resp.Written || resp.Manifest == nil {
		t.Fatalf("on: %+v", resp)
	}
	if _, err := os.Stat(resp.Manifest.SaveDir); err != nil {
		t.Errorf("on wrote no folder: %v", err)
	}
}

func listDir(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && strings.HasSuffix(p, ".db") || err == nil && d.IsDir() || err == nil && strings.Contains(p, "exports") {
			rel, _ := filepath.Rel(dir, p)
			out = append(out, rel)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(out)
	return out
}

// The same inputs give the same bytes and the same id; the files are not rewritten; a folder with the same
// name but other content is never overwritten.
func TestRerunIsIdempotentAndADifferingFolderIsNeverOverwritten(t *testing.T) {
	srv, w := newServer(t, "")
	seed(t, w, fixture())
	enableExport(t, w)
	args := map[string]any{"split_at": "2026-10-08"}

	first := srv.call(t, args).export(t)
	dir := first.Manifest.SaveDir
	snapshot := datasetFiles(t, dir)
	infoBefore, _ := os.Stat(filepath.Join(dir, "dataset_train.csv"))

	second := srv.call(t, args).export(t)
	if second.Written || second.Reason != datasetexport.ReasonAlreadyExported || second.Manifest == nil || second.Manifest.DatasetID != first.Manifest.DatasetID ||
		second.Manifest.DatasetSHA256 != first.Manifest.DatasetSHA256 {
		t.Fatalf("second call %+v", second)
	}
	for name, data := range datasetFiles(t, dir) {
		if !bytes.Equal(data, snapshot[name]) {
			t.Errorf("%s changed on a re-run", name)
		}
	}
	if infoAfter, _ := os.Stat(filepath.Join(dir, "dataset_train.csv")); !infoAfter.ModTime().Equal(infoBefore.ModTime()) {
		t.Error("the file was rewritten")
	}
	singleDataset(t, srv, first.Manifest.DatasetID)

	// An export of other content has another id, and sits beside the first.
	other := srv.call(t, map[string]any{"split_at": "2026-10-08", "embargo_days": 1}).export(t)
	if other.Manifest.DatasetID == first.Manifest.DatasetID || !other.Written {
		t.Errorf("another split shares the id: %+v", other.Manifest)
	}

	// Tamper with a file of the first folder (same id, different bytes): the call refuses and leaves it.
	tampered := []byte("not the dataset\n")
	path := filepath.Join(dir, "dataset_eval.csv")
	if err := os.WriteFile(path, tampered, 0o600); err != nil {
		t.Fatal(err)
	}
	third := srv.call(t, args).export(t)
	if third.Written || third.Reason != datasetexport.ReasonDirConflict || third.Manifest == nil || third.Manifest.DatasetID != first.Manifest.DatasetID {
		t.Fatalf("conflict response %+v", third)
	}
	if !bytes.Equal(readFile(t, path), tampered) {
		t.Error("the differing folder was overwritten")
	}
	if entries, _ := os.ReadDir(srv.exportsDir()); len(entries) != 2 {
		t.Errorf("a temporary folder or a second copy is left: %v", entries)
	}
}
