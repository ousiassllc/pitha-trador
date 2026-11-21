package report

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

var errDisk = errors.New("disk full")

// failOn fails the n-th write (0-based) after leaving a truncated file behind.
func failOn(n int) func(*os.File, []byte) error {
	calls := 0
	return func(f *os.File, data []byte) error {
		defer func() { calls++ }()
		if calls == n {
			_, _ = f.Write(data[:1])
			return errDisk
		}
		return writeData(f, data)
	}
}

func TestWriteAllRemovesWhatItCreatedWhenALaterWriteFails(t *testing.T) {
	for n := 0; n < 3; n++ {
		dir := t.TempDir()
		outputs := []output{
			{filepath.Join(dir, "a.json"), []byte("aaa")},
			{filepath.Join(dir, "b.md"), []byte("bbb")},
			{filepath.Join(dir, "c.manifest.json"), []byte("ccc")},
		}
		if err := writeAll(outputs, failOn(n)); !errors.Is(err, errDisk) {
			t.Fatalf("n=%d: err = %v, want %v", n, err, errDisk)
		}
		if left, _ := os.ReadDir(dir); len(left) != 0 {
			t.Fatalf("n=%d: %d file(s) left behind", n, len(left))
		}
		// The directory is free again: a rerun succeeds.
		if err := writeAll(outputs, writeData); err != nil {
			t.Fatalf("n=%d: rerun: %v", n, err)
		}
	}
}

func TestWriteAllKeepsAnExistingFile(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "b.md")
	if err := os.WriteFile(existing, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	outputs := []output{{filepath.Join(dir, "a.json"), []byte("aaa")}, {existing, []byte("bbb")}}
	if err := writeAll(outputs, writeData); !errors.Is(err, os.ErrExist) {
		t.Fatalf("err = %v, want ErrExist", err)
	}
	if b, _ := os.ReadFile(existing); string(b) != "old" {
		t.Fatalf("existing file was changed: %q", b)
	}
	if _, err := os.Lstat(filepath.Join(dir, "a.json")); !os.IsNotExist(err) {
		t.Fatalf("a.json from this call was not removed: %v", err)
	}
}
