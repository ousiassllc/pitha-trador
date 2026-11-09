package main

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/runner/runnertest"
)

// TestInitThenRunEndToEnd: init pre-registers the frozen manifest, run writes the
// three files; with the thresholds unset every verdict is 優劣未確定, the three
// populations stay apart and the source copy is byte-identical afterwards.
func TestInitThenRunEndToEnd(t *testing.T) {
	db := runnertest.Build(t, runnertest.Options{})
	before := sha(t, db.Path)
	out := filepath.Join(t.TempDir(), "out")

	initOut := mustCLI(t, initArgs(db.Path, out)...)
	if !strings.Contains(initOut, "min_days") {
		t.Errorf("init did not say the thresholds are unset: %s", initOut)
	}
	runOut := mustCLI(t, runArgs(db.Path, prereg(out), filepath.Join(out, "r1"))...)
	for _, want := range []string{"label layer: done", "replay layer: done"} {
		if !strings.Contains(runOut, want) {
			t.Errorf("run output lacks %q: %s", want, runOut)
		}
	}
	if after := sha(t, db.Path); after != before {
		t.Fatalf("the source copy changed: %s -> %s", before, after)
	}

	r := loadReport(t, filepath.Join(out, "r1", "metaablation-run1.json"))
	if r.Manifest.DBCopy.SHA256 != before || r.Manifest.Model.Version != db.ModelVersion || r.RunCount != 1 {
		t.Errorf("manifest = db %.8s model %s run %d", r.Manifest.DBCopy.SHA256, r.Manifest.Model.Version, r.RunCount)
	}
	if r.Manifest.Minimums.MinDays != nil || r.Manifest.Minimums.Power != nil {
		t.Error("the thresholds must stay unset")
	}
	if r.Manifest.Statistics.Seed != 1046 {
		t.Errorf("seed %d not recorded", r.Manifest.Statistics.Seed)
	}

	// Three populations, kept apart: (i) the original OOS reference, (ii) the label layer, (iii) the replay layer.
	var ids []string
	for _, p := range r.Populations {
		ids = append(ids, p.ID)
	}
	if !slices.Equal(ids, []string{"i", "ii", "iii"}) {
		t.Errorf("populations = %v", ids)
	}
	lr := r.Label.Result
	if !r.Label.Available || len(lr.Cells) != 4 || lr.Reference.Rows != 8 || lr.Independence.Independent != 32 || lr.Independence.Reference != 8 {
		t.Fatalf("label layer: available %v cells %d reference %d independence %+v", r.Label.Available, len(lr.Cells), lr.Reference.Rows, lr.Independence)
	}
	if !r.Replay.Available || len(r.Replay.Segments) != 1 || len(r.Replay.Segments[0].Cells) != 4 {
		t.Fatalf("replay layer: %+v", r.Replay)
	}

	// Every verdict is 優劣未確定; diagonals carry none.
	rows := append(slices.Clone(lr.Comparisons), r.Replay.Segments[0].Differences...)
	primary := 0
	for _, v := range rows {
		switch {
		case v.Reference && v.Verdict != "":
			t.Errorf("reference comparison %s has verdict %q", v.Name, v.Verdict)
		case !v.Reference && v.Verdict != "優劣未確定":
			t.Errorf("%s verdict %q, want 優劣未確定 (thresholds unset)", v.Name, v.Verdict)
		case !v.Reference:
			primary++
		}
	}
	if primary != 8 || r.Multiple.Executed != 8 || r.Multiple.FamilyExceeded {
		t.Errorf("primary %d executed %d exceeded %v, want 8/8/false", primary, r.Multiple.Executed, r.Multiple.FamilyExceeded)
	}

	// The replay stages follow the live application order.
	var order []string
	for _, s := range r.Replay.Segments[0].Cells[0].Stages {
		order = append(order, s.Stage)
	}
	want := []string{"decision", "eligibility", "quality", "flow_guards", "meta", "risk", "fill"}
	if !slices.Equal(order, want) {
		t.Errorf("replay stage order = %v, want %v", order, want)
	}
	// Label cells: Q off keeps the rows Q on drops (the same stage order is pinned in the library tests).
	cells := map[string]int{}
	for _, c := range lr.Cells {
		cells[c.Name] = c.Takes
	}
	if cells["m_off_q_off"] != 32 || cells["m_off_q_on"] != 16 || cells["m_on_q_off"] != 16 || cells["m_on_q_on"] != 8 {
		t.Errorf("takes per cell = %v, want 32/16/16/8", cells)
	}

	md := string(readFile(t, filepath.Join(out, "r1", "metaablation-run1.md")))
	for _, want := range []string{"優劣未確定", "proxy", "母集団は 3 段", "対角"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown lacks %q", want)
		}
	}
}

// TestOutputsCarryNoRawData: the aggregates hold no stored payload column name,
// no parameter body and no secret marker.
func TestOutputsCarryNoRawData(t *testing.T) {
	db := runnertest.Build(t, runnertest.Options{})
	out := filepath.Join(t.TempDir(), "out")
	mustCLI(t, initArgs(db.Path, out)...)
	mustCLI(t, runArgs(db.Path, prereg(out), filepath.Join(out, "r1"))...)
	for _, name := range []string{"metaablation-run1.json", "metaablation-run1.md", "metaablation-run1.manifest.json"} {
		low := strings.ToLower(string(readFile(t, filepath.Join(out, "r1", name))))
		for _, bad := range []string{"state_json", "response_json", "params_json", "logistic_regression", "weights", "api_key", "secret", "bearer ", "password", `"bid"`, `"ask"`} {
			if strings.Contains(low, bad) {
				t.Errorf("%s contains %q", name, bad)
			}
		}
	}
}

// TestRerunIsByteIdentical: the same frozen manifest on the same copy gives the
// same bytes (the seed is in the manifest).
func TestRerunIsByteIdentical(t *testing.T) {
	db := runnertest.Build(t, runnertest.Options{})
	out := filepath.Join(t.TempDir(), "out")
	mustCLI(t, initArgs(db.Path, out)...)
	mustCLI(t, runArgs(db.Path, prereg(out), filepath.Join(out, "a"))...)
	mustCLI(t, runArgs(db.Path, prereg(out), filepath.Join(out, "b"))...)
	for _, name := range []string{"metaablation-run1.json", "metaablation-run1.md", "metaablation-run1.manifest.json"} {
		if string(readFile(t, filepath.Join(out, "a", name))) != string(readFile(t, filepath.Join(out, "b", name))) {
			t.Errorf("%s differs between two runs of the same manifest", name)
		}
	}
}

// TestNewRunIncrementsTheRunCount: a changed threshold is a new run: the count
// goes to 2, the earlier run stays listed with its reason, and the pre-registration
// of the new run is written before anything is evaluated.
func TestNewRunIncrementsTheRunCount(t *testing.T) {
	db := runnertest.Build(t, runnertest.Options{})
	out := filepath.Join(t.TempDir(), "out")
	mustCLI(t, initArgs(db.Path, out)...)
	mustCLI(t, runArgs(db.Path, prereg(out), filepath.Join(out, "r1"))...)

	if _, err := cli(t, runArgs(db.Path, prereg(out), filepath.Join(out, "bad"), "-min-days", "2")...); err == nil || !strings.Contains(err.Error(), "new run") {
		t.Fatalf("a changed threshold without -new-run-reason must fail, got %v", err)
	}
	mustCLI(t, runArgs(db.Path, prereg(out), filepath.Join(out, "r2"), "-new-run-reason", "proposal confirmed", "-min-days", "2", "-min-count", "1")...)
	r := loadReport(t, filepath.Join(out, "r2", "metaablation-run2.json"))
	if r.RunCount != 2 || len(r.Manifest.Run.Previous) != 1 || r.Manifest.Run.Previous[0].ChangeReason != "proposal confirmed" {
		t.Errorf("run record = %d %+v", r.RunCount, r.Manifest.Run)
	}
	if r.Manifest.Minimums.MinDays == nil || *r.Manifest.Minimums.MinDays != 2 || r.Manifest.Minimums.Power != nil {
		t.Errorf("minimums = %+v", r.Manifest.Minimums)
	}
	pre := readFile(t, filepath.Join(out, "r2", "metaablation-run2.prereg.manifest.json"))
	if len(pre) == 0 {
		t.Error("the new run's pre-registration was not written")
	}
	// The earlier run's output is still there, unchanged.
	if r1 := loadReport(t, filepath.Join(out, "r1", "metaablation-run1.json")); r1.RunCount != 1 {
		t.Errorf("run 1 report was changed: %d", r1.RunCount)
	}
}
