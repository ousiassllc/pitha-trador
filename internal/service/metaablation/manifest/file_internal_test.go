package manifest

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteNewRemovesAHalfWrittenFileAndAllowsARerun(t *testing.T) {
	errDisk := errors.New("disk full")
	path := filepath.Join(t.TempDir(), "m.json")
	partial := func(f *os.File, b []byte) error {
		_, _ = f.Write(b[:1])
		return errDisk
	}
	if err := writeNew(path, []byte("{}\n"), partial); !errors.Is(err, errDisk) {
		t.Fatalf("err = %v, want %v", err, errDisk)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("half-written file left behind: %v", err)
	}
	if err := writeNew(path, []byte("{}\n"), writeData); err != nil {
		t.Fatalf("rerun: %v", err)
	}
}

func TestWriteNewNeverTouchesAnExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "m.json")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeNew(path, []byte("new"), writeData); !errors.Is(err, os.ErrExist) {
		t.Fatalf("err = %v, want ErrExist", err)
	}
	if b, _ := os.ReadFile(path); string(b) != "old" {
		t.Fatalf("existing file changed: %q", b)
	}
}
