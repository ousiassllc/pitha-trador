package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/manifest"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/runner/runnertest"
)

// initRun runs init then run on a fixture and returns the output directory of the run.
func initRun(t *testing.T, o runnertest.Options) (outRun string, runErr error) {
	t.Helper()
	db := runnertest.Build(t, o)
	out := filepath.Join(t.TempDir(), "out")
	mustCLI(t, initArgs(db.Path, out)...)
	outRun = filepath.Join(out, "r1")
	_, runErr = cli(t, runArgs(db.Path, prereg(out), outRun)...)
	return outRun, runErr
}

// TestUnreadableParamsIsNotExecutable: an unreadable params_json makes both
// layers 実行不可 with a reason, never a crash, and the body is not printed.
func TestUnreadableParamsIsNotExecutable(t *testing.T) {
	outRun, err := initRun(t, runnertest.Options{ParamsJSON: "this is not json SECRETBODY"})
	if err == nil {
		t.Error("run with no executable layer must exit non-zero")
	}
	r := loadReport(t, filepath.Join(outRun, "metaablation-run1.json"))
	if r.Label.Available || r.Replay.Available || !strings.Contains(r.Label.Reason, "実行不可") || !strings.Contains(r.Replay.Reason, "実行不可") {
		t.Fatalf("layers = label %v %q, replay %v %q", r.Label.Available, r.Label.Reason, r.Replay.Available, r.Replay.Reason)
	}
	for _, name := range []string{"metaablation-run1.json", "metaablation-run1.md", "metaablation-run1.manifest.json"} {
		if strings.Contains(string(readFile(t, filepath.Join(outRun, name))), "SECRETBODY") {
			t.Errorf("%s prints the parameter body", name)
		}
	}
}

// TestNoMarketSnapshotsLeavesTheLabelLayer: without market_snapshots the replay
// layer is not executable (with its reason) and the label layer still has its
// result; the rows it cannot rebuild are counted unknown and excluded from all cells.
func TestNoMarketSnapshotsLeavesTheLabelLayer(t *testing.T) {
	outRun, err := initRun(t, runnertest.Options{NoSnapshots: true})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	r := loadReport(t, filepath.Join(outRun, "metaablation-run1.json"))
	if r.Replay.Available || !strings.Contains(r.Replay.Reason, "market_snapshots") {
		t.Errorf("replay layer = %v %q", r.Replay.Available, r.Replay.Reason)
	}
	if !r.Label.Available || len(r.Label.Result.Cells) != 4 {
		t.Fatalf("label layer lost: %+v", r.Label)
	}
	if got := r.Label.Result.Cells[0].Funnel.Unknown; got != 32 {
		t.Errorf("unknown rows = %d, want all 32 independent rows (no snapshot to rebuild them from)", got)
	}
}

// TestMissingDayIsListedAndSplitsTheReplay: one evaluation day without bars is
// listed with its reason, the replay splits into two common segments (never
// stitched) and the label layer keeps the other days' results.
func TestMissingDayIsListedAndSplitsTheReplay(t *testing.T) {
	outRun, err := initRun(t, runnertest.Options{MissingBarDay: 11})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	r := loadReport(t, filepath.Join(outRun, "metaablation-run1.json"))
	if !r.Replay.Available || len(r.Replay.UnavailablePeriods) != 1 || r.Replay.UnavailablePeriods[0].From != "2026-10-30" {
		t.Fatalf("replay = %v periods %+v", r.Replay.Available, r.Replay.UnavailablePeriods)
	}
	if len(r.Replay.UnavailablePeriods[0].Reason) == 0 || len(r.Replay.Segments) != 2 {
		t.Errorf("segments = %d, reason %q", len(r.Replay.Segments), r.Replay.UnavailablePeriods[0].Reason)
	}
	if !r.Label.Available {
		t.Fatal("label layer lost")
	}
	c := r.Label.Result.Cells[0]
	if c.Funnel.Unknown != 8 || c.EligibleCandidates != 24 {
		t.Errorf("label cell: unknown %d eligible %d, want 8 and 24 (the day without bars is unknown, the rest is kept)", c.Funnel.Unknown, c.EligibleCandidates)
	}
}

// TestRuleOverridesAreRecordedAndReplayConfigIsPinned: a rule value given to init
// is in the frozen manifest and the run uses it; a manifest that does not pin
// the replay configuration (its fingerprint note removed) makes the replay layer
// not executable instead of replaying with an unpinned configuration.
func TestRuleOverridesAreRecordedAndReplayConfigIsPinned(t *testing.T) {
	db := runnertest.Build(t, runnertest.Options{})
	out := filepath.Join(t.TempDir(), "out")
	mustCLI(t, initArgs(db.Path, out, "-max-toxic-flow", "0.2", "-min-ev-bps", "1.5", "-min-days", "15")...)
	mustCLI(t, runArgs(db.Path, prereg(out), filepath.Join(out, "r1"))...)
	r := loadReport(t, filepath.Join(out, "r1", "metaablation-run1.json"))
	m := r.Manifest
	if m.Rules.Long.MaxToxicFlow != 0.2 || m.Rules.Short.MaxToxicFlow != 0.2 || m.Rules.MinEVBps != 1.5 || m.Minimums.MinDays == nil || *m.Minimums.MinDays != 15 {
		t.Errorf("rules not recorded: %+v %+v", m.Rules, m.Minimums)
	}
	if !r.Replay.Available {
		t.Errorf("replay: %s", r.Replay.Reason)
	}

	unpinned := filepath.Join(out, "unpinned.manifest.json")
	rewriteManifest(t, prereg(out), unpinned, func(m *manifest.Manifest) {
		var keep []string
		for _, n := range m.Provenance.Notes {
			if !strings.HasPrefix(n, "replay_config_sha256=") {
				keep = append(keep, n)
			}
		}
		m.Provenance.Notes = keep
	})
	mustCLI(t, runArgs(db.Path, unpinned, filepath.Join(out, "r2"))...)
	r = loadReport(t, filepath.Join(out, "r2", "metaablation-run1.json"))
	if r.Replay.Available || !strings.Contains(r.Replay.Reason, "指紋") || !r.Label.Available {
		t.Errorf("replay %v %q, label %v", r.Replay.Available, r.Replay.Reason, r.Label.Available)
	}
}
