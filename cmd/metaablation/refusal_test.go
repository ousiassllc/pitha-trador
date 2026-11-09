package main

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/manifest"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/runner"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/runner/runnertest"
)

// TestSecondInitContinuesTheRunChain: a changed rule value cannot start a fresh
// run 1. Without -supersedes it is refused; with it (and a reason) the chain
// count is 2, the earlier run is listed with its reason and its file is untouched.
func TestSecondInitContinuesTheRunChain(t *testing.T) {
	db := runnertest.Build(t, runnertest.Options{})
	out := filepath.Join(t.TempDir(), "out")
	mustCLI(t, initArgs(db.Path, out)...)
	first := readFile(t, prereg(out))

	_, err := cli(t, initArgs(db.Path, out, "-min-ev-bps", "2")...)
	if err == nil || !strings.Contains(err.Error(), "-supersedes") || exitCode(err) != exitRefused {
		t.Fatalf("a changed rule via a second init without -supersedes: %v (exit %d)", err, exitCode(err))
	}
	// A fresh output directory does not escape it when the earlier one is named.
	if _, err := cli(t, initArgs(db.Path, out, "-min-ev-bps", "2", "-supersedes", prereg(out))...); err == nil || !strings.Contains(err.Error(), "-new-run-reason") {
		t.Fatalf("-supersedes without a reason: %v", err)
	}
	if _, err := cli(t, initArgs(db.Path, out, "-min-ev-bps", "2", "-new-run-reason", "why")...); err == nil || !strings.Contains(err.Error(), "-supersedes") {
		t.Fatalf("-new-run-reason without -supersedes: %v", err)
	}

	mustCLI(t, initArgs(db.Path, out, "-min-ev-bps", "2", "-supersedes", prereg(out), "-new-run-reason", "ev floor raised")...)
	second, err := manifest.ReadFrozen(filepath.Join(out, "metaablation-run2.prereg.manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if second.Run.Count != 2 || len(second.Run.Previous) != 1 || second.Run.Previous[0].ChangeReason != "ev floor raised" || second.Rules.MinEVBps != 2 {
		t.Errorf("run record = %+v, min_ev %v", second.Run, second.Rules.MinEVBps)
	}
	if string(readFile(t, prereg(out))) != string(first) {
		t.Error("the earlier pre-registration was changed")
	}
	// The earlier one is no longer the latest: superseding it again is refused, the latest is accepted.
	if _, err := cli(t, initArgs(db.Path, out, "-supersedes", prereg(out), "-new-run-reason", "x")...); err == nil {
		t.Error("superseding a run that already has a successor was accepted")
	}
	mustCLI(t, initArgs(db.Path, out, "-supersedes", filepath.Join(out, "metaablation-run2.prereg.manifest.json"), "-new-run-reason", "again")...)
	third, err := manifest.ReadFrozen(filepath.Join(out, "metaablation-run3.prereg.manifest.json"))
	if err != nil || third.Run.Count != 3 || len(third.Run.Previous) != 2 {
		t.Errorf("third = %+v (%v)", third.Run, err)
	}

	// Another copy's manifest does not continue this chain.
	other := runnertest.Build(t, runnertest.Options{})
	otherOut := filepath.Join(t.TempDir(), "out")
	mustCLI(t, initArgs(other.Path, otherOut)...)
	if sha(t, db.Path) == sha(t, other.Path) {
		t.Fatal("the two fixtures are byte-identical")
	}
	if _, err := cli(t, initArgs(db.Path, filepath.Join(t.TempDir(), "o"), "-supersedes", prereg(otherOut), "-new-run-reason", "x")...); err == nil || exitCode(err) != exitRefused {
		t.Errorf("-supersedes of another copy: %v", err)
	}
}

// TestSymlinkIntoTheDatabaseDirectoryIsRejected: -out is compared after symlinks are resolved.
func TestSymlinkIntoTheDatabaseDirectoryIsRejected(t *testing.T) {
	db := runnertest.Build(t, runnertest.Options{})
	dbDir := filepath.Dir(db.Path)
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(dbDir, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	for name, out := range map[string]string{"the link": link, "below the link": filepath.Join(link, "reports")} {
		if _, err := cli(t, initArgs(db.Path, out)...); err == nil || exitCode(err) != exitRefused {
			t.Errorf("-out %s: %v", name, err)
		}
	}
	// The database path given through a link is the same file.
	dbLink := filepath.Join(t.TempDir(), "copy-link.db")
	if err := os.Symlink(db.Path, dbLink); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := cli(t, initArgs(dbLink, db.Path)...); err == nil {
		t.Error("-out = the database file through a link was accepted")
	}
	if _, err := os.Stat(filepath.Join(dbDir, "reports")); err == nil {
		t.Error("a rejected output directory was created")
	}
}

// TestACopyWithAnUnmergedWALIsRefused: the manifest hash covers the main file only.
func TestACopyWithAnUnmergedWALIsRefused(t *testing.T) {
	db := runnertest.Build(t, runnertest.Options{})
	out := filepath.Join(t.TempDir(), "out")
	mustCLI(t, initArgs(db.Path, out)...)
	// An empty -wal (what mode=ro leaves) is fine; a non-empty one is not.
	if err := os.WriteFile(db.Path+"-wal", nil, 0o600); err != nil {
		t.Fatal(err)
	}
	mustCLI(t, runArgs(db.Path, prereg(out), filepath.Join(out, "ok"))...)
	if err := os.WriteFile(db.Path+"-wal", []byte("unmerged"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := cli(t, initArgs(db.Path, filepath.Join(t.TempDir(), "o"))...); err == nil || !strings.Contains(err.Error(), "checkpoint") || exitCode(err) != exitRefused {
		t.Errorf("init with a non-empty wal: %v", err)
	}
	if _, err := cli(t, runArgs(db.Path, prereg(out), filepath.Join(out, "r"))...); err == nil || !strings.Contains(err.Error(), "checkpoint") || exitCode(err) != exitRefused {
		t.Errorf("run with a non-empty wal: %v", err)
	}
}

// TestInitRefusesAnEvaluationWindowInsideTheModelsWindows: the evaluation window must
// start at or after the model's original OOS end, and the embargo is not shortened.
func TestInitRefusesAnEvaluationWindowInsideTheModelsWindows(t *testing.T) {
	db := runnertest.Build(t, runnertest.Options{})
	args := func(from string, extra ...string) []string {
		return append([]string{"init", "-db", db.Path, "-out", filepath.Join(t.TempDir(), "o"), "-asof", asOf.Format(time.RFC3339),
			"-from", from, "-to", runnertest.Day(20).Format(time.RFC3339)}, extra...)
	}
	if _, err := cli(t, args(runnertest.Day(7).Format(time.RFC3339))...); err == nil || !strings.Contains(err.Error(), "original OOS end") {
		t.Errorf("-from inside the OOS window: %v", err)
	}
	mustCLI(t, args(runnertest.Day(8).Format(time.RFC3339))...)
	if _, err := cli(t, args(runnertest.Day(9).Format(time.RFC3339), "-embargo", "10m")...); err == nil || !strings.Contains(err.Error(), "embargo") {
		t.Errorf("-embargo below the default: %v", err)
	}
	mustCLI(t, args(runnertest.Day(9).Format(time.RFC3339), "-embargo", "45m")...)
}

// TestExitCodes: -h is 0, usage mistakes 2, refusals 3, a plain failure 1.
func TestExitCodes(t *testing.T) {
	db := runnertest.Build(t, runnertest.Options{})
	out := filepath.Join(t.TempDir(), "out")
	mustCLI(t, initArgs(db.Path, out)...)
	for name, tc := range map[string]struct {
		args []string
		want int
	}{
		"help":                 {[]string{"init", "-h"}, 0},
		"no subcommand":        {nil, exitUsage},
		"unknown subcommand":   {[]string{"nope"}, exitUsage},
		"unknown flag":         {[]string{"run", "-nope"}, exitUsage},
		"missing -db":          {[]string{"run", "-manifest", "x"}, exitUsage},
		"bad time":             {[]string{"init", "-db", db.Path, "-out", out, "-asof", "yesterday", "-from", "2026-10-28", "-to", "2026-11-08"}, exitUsage},
		"missing manifest":     {runArgs(db.Path, filepath.Join(out, "absent.json"), filepath.Join(out, "r")), exitFailure},
		"out is the database":  {runArgs(db.Path, prereg(out), db.Path), exitRefused},
		"value change no flag": {runArgs(db.Path, prereg(out), filepath.Join(out, "r"), "-min-days", "2"), exitRefused},
	} {
		_, err := cli(t, tc.args...)
		if got := exitCode(err); got != tc.want {
			t.Errorf("%s: exit %d (%v), want %d", name, got, err, tc.want)
		}
	}
	// A changed manifest and a model mismatch are refusals too.
	raw := string(readFile(t, prereg(out)))
	tampered := filepath.Join(out, "tampered.json")
	if err := os.WriteFile(tampered, []byte(strings.Replace(raw, `"min_days": null`, `"min_days": 2`, 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := cli(t, runArgs(db.Path, tampered, filepath.Join(out, "t"))...); exitCode(err) != exitRefused {
		t.Errorf("tampered manifest: exit %d (%v)", exitCode(err), err)
	}
	if exitCode(flag.ErrHelp) != 0 || exitCode(runner.ErrRefused) != exitRefused || exitCode(manifest.ErrModelMismatch) != exitRefused {
		t.Error("exitCode mapping")
	}
}

// TestNotExecutableExitCode: no executable layer is exit 4.
func TestNotExecutableExitCode(t *testing.T) {
	_, err := initRun(t, runnertest.Options{ParamsJSON: "not json"})
	if exitCode(err) != exitNotExecutable {
		t.Errorf("exit %d (%v), want %d", exitCode(err), err, exitNotExecutable)
	}
}

// TestPartialOutputSetIsNeverWritten: when one of the three output files already
// exists the run writes none of them.
func TestPartialOutputSetIsNeverWritten(t *testing.T) {
	db := runnertest.Build(t, runnertest.Options{})
	out := filepath.Join(t.TempDir(), "out")
	mustCLI(t, initArgs(db.Path, out)...)
	r := filepath.Join(out, "r1")
	if err := os.MkdirAll(r, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r, "metaablation-run1.manifest.json"), []byte("earlier"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := cli(t, runArgs(db.Path, prereg(out), r)...); err == nil {
		t.Fatal("run overwrote an existing output")
	}
	for _, name := range []string{"metaablation-run1.json", "metaablation-run1.md"} {
		if _, err := os.Stat(filepath.Join(r, name)); err == nil {
			t.Errorf("%s was written although another output already existed", name)
		}
	}
	if string(readFile(t, filepath.Join(r, "metaablation-run1.manifest.json"))) != "earlier" {
		t.Error("the existing output was changed")
	}
}

// TestCodeVersionIsStampedAndAMismatchIsNoted: init records a code version (the
// git HEAD when the build has no stamp), and a run by other code says so in the report.
func TestCodeVersionIsStampedAndAMismatchIsNoted(t *testing.T) {
	if v := runner.CodeVersion(); v == "" {
		t.Fatal("empty code version")
	}
	db := runnertest.Build(t, runnertest.Options{})
	out := filepath.Join(t.TempDir(), "out")
	mustCLI(t, initArgs(db.Path, out)...)
	mustCLI(t, runArgs(db.Path, prereg(out), filepath.Join(out, "same"))...)
	same := readFile(t, filepath.Join(out, "same", "metaablation-run1.json"))
	if strings.Contains(string(same), "コードの版が manifest と違う") {
		t.Error("a note about the code version without a difference")
	}
	other := filepath.Join(out, "other.manifest.json")
	rewriteManifest(t, prereg(out), other, func(m *manifest.Manifest) { m.CodeVersion = "someone-else" })
	mustCLI(t, runArgs(db.Path, other, filepath.Join(out, "diff"))...)
	r := string(readFile(t, filepath.Join(out, "diff", "metaablation-run1.md")))
	if !strings.Contains(r, "コードの版が manifest と違う") || !strings.Contains(r, "someone-else") {
		t.Error("the report does not note the different code version")
	}
}
