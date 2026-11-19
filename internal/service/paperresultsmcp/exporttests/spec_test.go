package exporttests

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/datasetexport"
)

// specText is the part of the dataset hash that is not a file: the schema version, then the period and the split.
func specText(schemaVersion, from, to, splitAt string, embargoDays int) string {
	return schemaVersion + "\nfrom=" + from + "\nto=" + to + "\nsplit_at=" + splitAt + "\nembargo_days=" + strconv.Itoa(embargoDays) + "\n"
}

// Two exports that differ only in the period or the split get other dataset ids even when their files hold
// the same bytes, and each manifest (the response and the file) says the period and split it was made with.
func TestAnotherPeriodOrSplitIsAnotherDataset(t *testing.T) {
	srv, w := newServer(t, "")
	var decs []seedDec
	for _, d := range fixture() {
		if d.inPeriod() {
			decs = append(decs, d)
		}
	}
	seed(t, w, decs)
	enableExport(t, w)

	type spec struct {
		name    string
		args    map[string]any
		from    string
		to      string
		splitAt string
		embargo int
	}
	// Every pair differs in one of from, to, split_at, embargo_days, and no decision sits in the days they add or move.
	specs := []spec{
		{name: "base", args: nil, from: periodFrom, to: periodTo},
		{name: "wider to", args: map[string]any{"to": "2026-10-14"}, from: periodFrom, to: "2026-10-14"},
		{name: "earlier from", args: map[string]any{"from": "2026-10-01"}, from: "2026-10-01", to: periodTo},
		{name: "split a", args: map[string]any{"to": "2026-10-14", "split_at": "2026-10-12"}, from: periodFrom, to: "2026-10-14", splitAt: "2026-10-12"},
		{name: "split b", args: map[string]any{"to": "2026-10-14", "split_at": "2026-10-13"}, from: periodFrom, to: "2026-10-14", splitAt: "2026-10-13"},
		{name: "split a embargo", args: map[string]any{"to": "2026-10-14", "split_at": "2026-10-12", "embargo_days": 1}, from: periodFrom, to: "2026-10-14", splitAt: "2026-10-12", embargo: 1},
	}
	ids := map[string]string{}
	var baseCSV []byte
	for _, s := range specs {
		resp := srv.call(t, s.args).export(t)
		m := resp.Manifest
		if !resp.Written || m == nil || resp.Reason != "" {
			t.Fatalf("%s: response %+v", s.name, resp)
		}
		if other, dup := ids[m.DatasetID]; dup {
			t.Errorf("%s has the id of %s (%s): the old specification's manifest would be returned", s.name, other, m.DatasetID)
		}
		ids[m.DatasetID] = s.name

		// The manifest holds the specification it was made with, in the response and in the file.
		var onDisk datasetexport.Manifest
		if err := json.Unmarshal(readFile(t, filepath.Join(m.SaveDir, "manifest.json")), &onDisk); err != nil {
			t.Fatal(err)
		}
		for label, mm := range map[string]*datasetexport.Manifest{"response": m, "file": &onDisk} {
			var gotSplit string
			if mm.SplitAt != nil {
				gotSplit = *mm.SplitAt
			}
			if mm.From != s.from || mm.To != s.to || gotSplit != s.splitAt || mm.EmbargoDays != s.embargo || (mm.SplitAt == nil) != (s.splitAt == "") {
				t.Errorf("%s %s manifest says %s..%s split %v embargo %d", s.name, label, mm.From, mm.To, mm.SplitAt, mm.EmbargoDays)
			}
		}
		// The hash is the spec and the files: it reproduces from the manifest.
		var lines string
		for _, f := range m.Files {
			lines += f.Name + " " + f.SHA256 + "\n"
		}
		if want := sha([]byte(specText(m.SchemaVersion, s.from, s.to, s.splitAt, s.embargo) + lines)); m.DatasetSHA256 != want {
			t.Errorf("%s: dataset hash %s, want %s", s.name, m.DatasetSHA256, want)
		}
		// The point of the test: without a split the files are the same bytes whatever the period.
		if s.splitAt == "" {
			csv := readFile(t, filepath.Join(m.SaveDir, "dataset.csv"))
			if baseCSV == nil {
				baseCSV = csv
			} else if !bytes.Equal(csv, baseCSV) {
				t.Errorf("%s: the CSV differs, the test no longer shows a spec-only difference", s.name)
			}
		}
	}
	if len(ids) != len(specs) {
		t.Errorf("%d ids for %d specifications", len(ids), len(specs))
	}
	if entries, _ := os.ReadDir(srv.exportsDir()); len(entries) != len(specs) {
		t.Errorf("exports folder holds %d datasets, want %d", len(entries), len(specs))
	}

	// The same specification again is the same dataset: the same id, nothing rewritten, its own manifest returned.
	again := srv.call(t, specs[1].args).export(t)
	if again.Written || again.Reason != datasetexport.ReasonAlreadyExported || again.Manifest.To != "2026-10-14" || again.Manifest.DatasetID == "" {
		t.Errorf("same specification again: %+v", again)
	}
	if ids[again.Manifest.DatasetID] != "wider to" {
		t.Errorf("same specification got the id of %q", ids[again.Manifest.DatasetID])
	}
}

// A folder whose manifest states the right hash but another period (a manifest of an old specification put
// under this id, or an edited one) is not taken for this dataset: it is a conflict and stays as it is.
func TestAManifestOfAnotherSpecificationIsNotTheSameDataset(t *testing.T) {
	srv, w := newServer(t, "")
	seed(t, w, fixture())
	enableExport(t, w)
	first := srv.call(t, nil).export(t).Manifest
	path := filepath.Join(first.SaveDir, "manifest.json")
	var edited map[string]any
	if err := json.Unmarshal(readFile(t, path), &edited); err != nil {
		t.Fatal(err)
	}
	edited["to"] = "2026-10-13"
	body, _ := json.Marshal(edited)
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	resp := srv.call(t, nil).export(t)
	if resp.Written || resp.Reason != datasetexport.ReasonDirConflict || resp.Manifest == nil || resp.Manifest.To != periodTo {
		t.Fatalf("response %+v", resp)
	}
	if !bytes.Equal(readFile(t, path), body) {
		t.Error("the folder was overwritten")
	}
}
