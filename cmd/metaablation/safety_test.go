package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/loader"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/manifest"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/runner/runnertest"
)

// TestDatabaseIsOpenedReadOnly: a write through the opener fails, and the file's
// content and mtime are unchanged by a whole init and run.
func TestDatabaseIsOpenedReadOnly(t *testing.T) {
	db := runnertest.Build(t, runnertest.Options{})
	src, err := loader.Open(db.Path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := src.DB().Exec(`INSERT INTO runtime_settings (key, value, updated_at) VALUES ('x', 'y', '2026-01-01T00:00:00Z')`); err == nil {
		t.Error("a write through the read-only opener succeeded")
	}
	if _, err := src.DB().Exec(`UPDATE metalabel_models SET status = 'active'`); err == nil {
		t.Error("promoting a model through the read-only opener succeeded")
	}
	_ = src.Close()

	st0, err := os.Stat(db.Path)
	if err != nil {
		t.Fatal(err)
	}
	h0 := sha(t, db.Path)
	out := filepath.Join(t.TempDir(), "out")
	mustCLI(t, initArgs(db.Path, out)...)
	mustCLI(t, runArgs(db.Path, prereg(out), filepath.Join(out, "r1"))...)
	st1, err := os.Stat(db.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !st1.ModTime().Equal(st0.ModTime()) || st1.Size() != st0.Size() || sha(t, db.Path) != h0 {
		t.Error("the database file changed")
	}
	// The model's status and the settings are untouched.
	src, err = loader.Open(db.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = src.Close() }()
	var status string
	if err := src.DB().QueryRow(`SELECT status FROM metalabel_models WHERE version = ?`, db.ModelVersion).Scan(&status); err != nil || status != "rejected" {
		t.Errorf("model status = %q (%v)", status, err)
	}
}

// TestOutputDirectoryMustBeOutsideTheDatabaseDirectory.
func TestOutputDirectoryMustBeOutsideTheDatabaseDirectory(t *testing.T) {
	db := runnertest.Build(t, runnertest.Options{})
	dbDir := filepath.Dir(db.Path)
	for name, out := range map[string]string{
		"the database file itself": db.Path,
		"the database directory":   dbDir,
		"inside the directory":     filepath.Join(dbDir, "reports"),
		"empty":                    "",
	} {
		if _, err := cli(t, initArgs(db.Path, out)...); err == nil {
			t.Errorf("init accepted -out = %s", name)
		}
	}
	if _, err := os.Stat(filepath.Join(dbDir, "reports")); err == nil {
		t.Error("a rejected output directory was created")
	}
	// run rejects it too (the manifest comes from a valid init elsewhere).
	good := filepath.Join(t.TempDir(), "out")
	mustCLI(t, initArgs(db.Path, good)...)
	if _, err := cli(t, runArgs(db.Path, prereg(good), db.Path)...); err == nil {
		t.Error("run accepted -out = the database file")
	}
	if _, err := cli(t, runArgs(db.Path, prereg(good), filepath.Join(dbDir, "r"))...); err == nil {
		t.Error("run accepted -out inside the database directory")
	}
}

// rewriteManifest freezes mut(copy of the manifest at mf) again as a new file:
// what init would have written had it hashed another copy.
func rewriteManifest(t *testing.T, mf, to string, mut func(*manifest.Manifest)) {
	t.Helper()
	m, err := manifest.ReadFrozen(mf)
	if err != nil {
		t.Fatal(err)
	}
	at := m.Frozen.At
	m.Frozen = nil
	mut(&m)
	if err := m.Freeze(at); err != nil {
		t.Fatal(err)
	}
	if err := manifest.WriteNew(to, m); err != nil {
		t.Fatal(err)
	}
}

// TestRunFailsLoudlyWhenTheCopyOrTheManifestChanged.
func TestRunFailsLoudlyWhenTheCopyOrTheManifestChanged(t *testing.T) {
	db := runnertest.Build(t, runnertest.Options{})
	out := filepath.Join(t.TempDir(), "out")
	mustCLI(t, initArgs(db.Path, out)...)

	// A tampered manifest is refused.
	raw := readFile(t, prereg(out))
	tampered := filepath.Join(out, "tampered.json")
	if err := os.WriteFile(tampered, []byte(strings.Replace(string(raw), `"min_days": null`, `"min_days": 2`, 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := cli(t, runArgs(db.Path, tampered, filepath.Join(out, "t"))...); err == nil || !strings.Contains(err.Error(), "changed after freeze") {
		t.Errorf("tampered manifest: %v", err)
	}
	// A manifest that is not frozen is refused.
	unfrozen := filepath.Join(out, "unfrozen.json")
	if err := os.WriteFile(unfrozen, []byte(strings.Replace(string(raw), `"frozen"`, `"x_frozen"`, 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := cli(t, runArgs(db.Path, unfrozen, filepath.Join(out, "u"))...); err == nil {
		t.Error("an unreadable manifest was accepted")
	}

	// Another copy (one more rejected model) is not the copy the manifest names.
	runnertest.AddRejectedModel(t, db.Path, "lr-synth-2", runnertest.Day(12))
	_, err := cli(t, runArgs(db.Path, prereg(out), filepath.Join(out, "r1"))...)
	if err == nil || !strings.Contains(err.Error(), "SHA-256") {
		t.Errorf("changed copy: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(out, "r1")); statErr == nil {
		t.Error("a failed run wrote output")
	}
}
