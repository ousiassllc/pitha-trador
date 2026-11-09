package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/manifest"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/runner/runnertest"
)

// TestTargetIsTheLatestRejectedModelAtInitAndNeverReplaced: init pins the newest
// rejected model of that moment; a newer one stored afterwards is not picked up
// by run (the manifest names the version), while a later init does pin it.
func TestTargetIsTheLatestRejectedModelAtInitAndNeverReplaced(t *testing.T) {
	db := runnertest.Build(t, runnertest.Options{})
	// An older rejected model exists too: it is not the latest.
	runnertest.AddRejectedModel(t, db.Path, "lr-synth-old", runnertest.Day(8))
	out := filepath.Join(t.TempDir(), "out")
	mustCLI(t, initArgs(db.Path, out)...)
	m, err := manifest.ReadFrozen(prereg(out))
	if err != nil {
		t.Fatal(err)
	}
	if m.Model.Version != "lr-synth-1" {
		t.Fatalf("pinned %q, want the latest rejected lr-synth-1 (trained day 9)", m.Model.Version)
	}

	// A newer model arrives. The copy changed, so the old manifest is refused ...
	runnertest.AddRejectedModel(t, db.Path, "lr-synth-newer", runnertest.Day(12))
	if _, err := cli(t, runArgs(db.Path, prereg(out), filepath.Join(out, "x"))...); err == nil {
		t.Fatal("run accepted a copy other than the one the manifest names")
	}
	// ... and a manifest frozen for the changed copy still names lr-synth-1: run does not pick the newer model.
	again := filepath.Join(out, "again.manifest.json")
	rewriteManifest(t, prereg(out), again, func(m *manifest.Manifest) { m.DBCopy.SHA256 = sha(t, db.Path) })
	mustCLI(t, runArgs(db.Path, again, filepath.Join(out, "r1"))...)
	r := loadReport(t, filepath.Join(out, "r1", "metaablation-run1.json"))
	if r.Manifest.Model.Version != "lr-synth-1" {
		t.Errorf("run used %q", r.Manifest.Model.Version)
	}
	for _, name := range []string{"metaablation-run1.json", "metaablation-run1.md"} {
		if strings.Contains(string(readFile(t, filepath.Join(out, "r1", name))), "lr-synth-newer") {
			t.Errorf("%s mentions the newer model", name)
		}
	}
	// A fresh init (a new study) pins the newer one.
	out2 := filepath.Join(t.TempDir(), "out2")
	mustCLI(t, initArgs(db.Path, out2)...)
	if m2, err := manifest.ReadFrozen(prereg(out2)); err != nil || m2.Model.Version != "lr-synth-newer" {
		t.Errorf("second init pinned %+v (%v)", m2.Model, err)
	}
}

// TestRunRefusesAModelWhoseParametersChanged: the stored parameters are hashed
// into the manifest; a copy whose model parameters differ is not re-scored.
func TestRunRefusesAModelWhoseParametersChanged(t *testing.T) {
	db := runnertest.Build(t, runnertest.Options{})
	out := filepath.Join(t.TempDir(), "out")
	mustCLI(t, initArgs(db.Path, out)...)

	conn, err := sqlitedb.Open(db.Path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`UPDATE metalabel_models SET params_json = replace(params_json, '1.5', '0.5')`); err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
	again := filepath.Join(out, "again.manifest.json")
	rewriteManifest(t, prereg(out), again, func(m *manifest.Manifest) { m.DBCopy.SHA256 = sha(t, db.Path) })
	if _, err := cli(t, runArgs(db.Path, again, filepath.Join(out, "r1"))...); err == nil || !strings.Contains(err.Error(), "differs from the frozen model") {
		t.Errorf("changed parameters: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(out, "r1")); statErr == nil {
		t.Error("a refused run wrote output")
	}
}

// TestInitNeedsACohort: a model stored without its trained cohort cannot be
// evaluated without an explicit, recorded -cohort.
func TestInitNeedsACohort(t *testing.T) {
	db := runnertest.Build(t, runnertest.Options{NoCohort: true})
	out := filepath.Join(t.TempDir(), "out")
	if _, err := cli(t, initArgs(db.Path, out)...); err == nil || !strings.Contains(err.Error(), "-cohort") {
		t.Fatalf("init without a cohort: %v", err)
	}
	if _, err := os.Stat(prereg(out)); err == nil {
		t.Fatal("a manifest was written without a cohort")
	}
	mustCLI(t, initArgs(db.Path, out, "-cohort", "entry-quality-v3")...)
	if m, err := manifest.ReadFrozen(prereg(out)); err != nil || m.Cohort != "entry-quality-v3" {
		t.Errorf("cohort = %q (%v)", m.Cohort, err)
	}
	// An explicit cohort never contradicts the recorded one.
	db2 := runnertest.Build(t, runnertest.Options{})
	if _, err := cli(t, initArgs(db2.Path, filepath.Join(t.TempDir(), "o"), "-cohort", "entry-quality-v4")...); err == nil {
		t.Error("-cohort contradicting the model's recorded cohort was accepted")
	}
}
