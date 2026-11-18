package datasetexport

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// An exports folder that already exists as a symlink, or that others can read, is refused before anything is written.
func TestPublishRefusesANonPrivateExportsFolder(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits are not meaningful on Windows")
	}
	root := t.TempDir()
	link := filepath.Join(root, "link")
	if err := os.Symlink(t.TempDir(), link); err != nil {
		t.Fatal(err)
	}
	open := filepath.Join(root, "open")
	if err := os.Mkdir(open, 0o755); err != nil { //nolint:gosec // G301: the point of the test
		t.Fatal(err)
	}
	for name, base := range map[string]string{"symlink": link, "world-readable": open} {
		if _, _, err := publish(base, "ds-x", nil, Manifest{}); err == nil {
			t.Errorf("%s: publish into %s succeeded", name, base)
		}
		if entries, _ := os.ReadDir(base); len(entries) != 0 {
			t.Errorf("%s: something was written: %v", name, entries)
		}
	}
}
