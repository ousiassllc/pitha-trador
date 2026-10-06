package tempcleanup_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/updater/tempcleanup"
)

func mkDir(t *testing.T, root, name string, modTime time.Time) string {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "installer.exe"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(dir, modTime, modTime); err != nil {
		t.Fatal(err)
	}
	return dir
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestCleanupStale_RemovesOnlyOlderUpdateDirs(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp) // os.TempDir on Unix
	t.Setenv("TMP", tmp)    // ... and on Windows
	t.Setenv("TEMP", tmp)

	startedAt := time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC)
	before := startedAt.Add(-time.Hour)
	stale1 := mkDir(t, tmp, "pitha-trador-update-111", before)
	stale2 := mkDir(t, tmp, "pitha-trador-update-222", before)
	fresh := mkDir(t, tmp, "pitha-trador-update-333", startedAt.Add(time.Minute))
	other := mkDir(t, tmp, "unrelated-dir", before)
	lookalike := mkDir(t, tmp, "xpitha-trador-update-444", before)
	file := filepath.Join(tmp, "pitha-trador-update-file")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(file, before, before); err != nil {
		t.Fatal(err)
	}

	removed, err := tempcleanup.CleanupStale(startedAt)
	if err != nil {
		t.Fatalf("CleanupStale: %v", err)
	}
	if removed != 2 {
		t.Errorf("removed = %d, want 2", removed)
	}
	for _, gone := range []string{stale1, stale2} {
		if exists(gone) {
			t.Errorf("%s still exists, want removed", gone)
		}
	}
	for _, kept := range []string{fresh, other, lookalike, file} {
		if !exists(kept) {
			t.Errorf("%s was removed, want kept", kept)
		}
	}
}

func TestCleanupStale_NothingToRemove(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	t.Setenv("TMP", tmp)
	t.Setenv("TEMP", tmp)
	removed, err := tempcleanup.CleanupStale(time.Now())
	if err != nil || removed != 0 {
		t.Errorf("CleanupStale = %d, %v; want 0, nil", removed, err)
	}
}
