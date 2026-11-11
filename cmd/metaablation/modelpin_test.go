package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/manifest"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/runner/runnertest"
)

// withNewerModel stores a rejected model trained after lr-synth-1 whose original
// OOS ends on day 10: the nightly retraining that moves "the latest rejected
// model" and with it the earliest valid -from.
func withNewerModel(t *testing.T, path string) {
	t.Helper()
	runnertest.AddModel(t, path, runnertest.ModelSpec{Version: "lr-synth-newer", TrainedAt: runnertest.Day(12), OOSEnd: runnertest.Day(10), Bias: 1})
}

// TestModelFlagPinsAnOlderRejectedModel: with a newer rejected model in the copy,
// the default still pins the newest one (and its later OOS end makes -from day 9
// invalid), while -model <older> pins that model, takes ITS OOS end as the floor,
// is recorded as explicit, and runs.
func TestModelFlagPinsAnOlderRejectedModel(t *testing.T) {
	db := runnertest.Build(t, runnertest.Options{})
	withNewerModel(t, db.Path)

	if _, err := cli(t, initArgs(db.Path, filepath.Join(t.TempDir(), "o"))...); err == nil || !strings.Contains(err.Error(), "original OOS end") {
		t.Fatalf("default pin of the newer model with -from before its OOS end: %v", err)
	}

	out := filepath.Join(t.TempDir(), "out")
	if o := mustCLI(t, initArgs(db.Path, out, "-model", "lr-synth-1")...); !strings.Contains(o, "lr-synth-1 (explicit)") {
		t.Errorf("init output does not name the pinned model and how: %s", o)
	}
	m, err := manifest.ReadFrozen(prereg(out))
	if err != nil {
		t.Fatal(err)
	}
	if m.Model.Version != "lr-synth-1" || m.Model.Selection != manifest.SelectionExplicit || !m.Windows.OOSEnd.Equal(runnertest.Day(8)) {
		t.Fatalf("manifest model %+v, oos end %s", m.Model, m.Windows.OOSEnd)
	}
	mustCLI(t, runArgs(db.Path, prereg(out), filepath.Join(out, "r1"))...)
	r := loadReport(t, filepath.Join(out, "r1", "metaablation-run1.json"))
	if r.Manifest.Model.Version != "lr-synth-1" || !r.Label.Available || !r.Replay.Available {
		t.Errorf("run used %q, label %v, replay %v", r.Manifest.Model.Version, r.Label.Available, r.Replay.Available)
	}
	md := string(readFile(t, filepath.Join(out, "r1", "metaablation-run1.md")))
	if strings.Contains(md, "lr-synth-newer") || !strings.Contains(md, manifest.SelectionExplicit) {
		t.Error("the report must name the pinned model and how it was chosen, and not the newer one")
	}

	// supersedes continues the same study (same copy, same model) although a newer model exists ...
	mustCLI(t, initArgs(db.Path, out, "-model", "lr-synth-1", "-min-ev-bps", "2", "-supersedes", prereg(out), "-new-run-reason", "ev floor raised")...)
	if m2, err := manifest.ReadFrozen(filepath.Join(out, "metaablation-run2.prereg.manifest.json")); err != nil || m2.Run.Count != 2 || m2.Model.Version != "lr-synth-1" {
		t.Errorf("second run = %+v (%v)", m2.Run, err)
	}
	// ... while an init without -model would be the newer model: another study.
	out2 := filepath.Join(t.TempDir(), "out2")
	mustCLI(t, initArgs(db.Path, out2, "-from", runnertest.Day(10).Format(time.RFC3339))...)
	if _, err := cli(t, initArgs(db.Path, out2, "-from", runnertest.Day(10).Format(time.RFC3339), "-supersedes", prereg(out), "-new-run-reason", "x")...); exitCode(err) != exitRefused {
		t.Errorf("superseding a manifest of another model: exit %d (%v)", exitCode(err), err)
	}
}

// TestDefaultInitRecordsLatestRejected: without -model the model and window are
// the same as before, and the manifest says so.
func TestDefaultInitRecordsLatestRejected(t *testing.T) {
	db := runnertest.Build(t, runnertest.Options{})
	out := filepath.Join(t.TempDir(), "out")
	mustCLI(t, initArgs(db.Path, out)...)
	m, err := manifest.ReadFrozen(prereg(out))
	if err != nil {
		t.Fatal(err)
	}
	if m.Model.Version != "lr-synth-1" || m.Model.Selection != manifest.SelectionLatestRejected {
		t.Errorf("model = %+v", m.Model)
	}
}

// TestModelFlagKeepsTheOOSEndFloor: -from before the named model's original OOS
// end is refused; at its end it is accepted.
func TestModelFlagKeepsTheOOSEndFloor(t *testing.T) {
	db := runnertest.Build(t, runnertest.Options{})
	withNewerModel(t, db.Path)
	args := func(model, from string) []string {
		return []string{"init", "-db", db.Path, "-out", filepath.Join(t.TempDir(), "o"), "-asof", asOf.Format(time.RFC3339),
			"-from", from, "-to", runnertest.Day(20).Format(time.RFC3339), "-model", model}
	}
	if _, err := cli(t, args("lr-synth-1", runnertest.Day(7).Format(time.RFC3339))...); err == nil || !strings.Contains(err.Error(), "original OOS end") {
		t.Errorf("-from before the named model's OOS end: %v", err)
	}
	if _, err := cli(t, args("lr-synth-newer", runnertest.Day(9).Format(time.RFC3339))...); err == nil || !strings.Contains(err.Error(), "original OOS end") {
		t.Errorf("-from before the newer named model's OOS end: %v", err)
	}
	mustCLI(t, args("lr-synth-newer", runnertest.Day(10).Format(time.RFC3339))...)
}

// TestModelFlagRefusesAVersionThatCannotBeScored: a version that is not stored, is
// not a rejected model, or has unreadable parameters is a refusal (exit 3) and no
// output directory is created.
func TestModelFlagRefusesAVersionThatCannotBeScored(t *testing.T) {
	db := runnertest.Build(t, runnertest.Options{})
	runnertest.AddModel(t, db.Path, runnertest.ModelSpec{Version: "lr-synth-broken", TrainedAt: runnertest.Day(7), ParamsJSON: "this is not json SECRETBODY"})
	for name, version := range map[string]string{"missing": "lr-nope", "no coefficients": "lr-synth-broken"} {
		out := filepath.Join(t.TempDir(), "out")
		o, err := cli(t, initArgs(db.Path, out, "-model", version)...)
		if exitCode(err) != exitRefused {
			t.Errorf("%s: exit %d (%v), want %d", name, exitCode(err), err, exitRefused)
		}
		if _, statErr := os.Stat(out); statErr == nil {
			t.Errorf("%s: the output directory was created", name)
		}
		if strings.Contains(o+errString(err), "SECRETBODY") {
			t.Errorf("%s: the parameter body is printed", name)
		}
	}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// TestRunFollowsTheRecordedModelNotTheLatest: a copy that gained a newer model
// after init (so a different file) runs for the manifest frozen to it; a manifest
// naming another model is refused (exit 3).
func TestRunFollowsTheRecordedModelNotTheLatest(t *testing.T) {
	db := runnertest.Build(t, runnertest.Options{})
	out := filepath.Join(t.TempDir(), "out")
	mustCLI(t, initArgs(db.Path, out, "-model", "lr-synth-1")...)

	withNewerModel(t, db.Path)
	// The copy changed: the old manifest is refused until it is frozen for the new copy.
	if _, err := cli(t, runArgs(db.Path, prereg(out), filepath.Join(out, "x"))...); exitCode(err) != exitRefused {
		t.Fatalf("manifest of the old copy: exit %d (%v)", exitCode(err), err)
	}
	same := filepath.Join(out, "same-model.manifest.json")
	rewriteManifest(t, prereg(out), same, func(m *manifest.Manifest) { m.DBCopy.SHA256 = sha(t, db.Path) })
	mustCLI(t, runArgs(db.Path, same, filepath.Join(out, "r1"))...)
	if r := loadReport(t, filepath.Join(out, "r1", "metaablation-run1.json")); r.Manifest.Model.Version != "lr-synth-1" {
		t.Errorf("run used %q", r.Manifest.Model.Version)
	}

	other := filepath.Join(out, "other-model.manifest.json")
	rewriteManifest(t, prereg(out), other, func(m *manifest.Manifest) {
		m.DBCopy.SHA256 = sha(t, db.Path)
		m.Model.Version = "lr-synth-newer" // the params hash stays lr-synth-1's
	})
	if _, err := cli(t, runArgs(db.Path, other, filepath.Join(out, "r2"))...); exitCode(err) != exitRefused {
		t.Errorf("manifest naming another model: exit %d (%v)", exitCode(err), err)
	}
	if _, statErr := os.Stat(filepath.Join(out, "r2")); statErr == nil {
		t.Error("a refused run wrote output")
	}
}
