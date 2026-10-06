package logging

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A failing fsync / close of the archive must keep the original log and
// leave no partial .gz behind (issue #620).
func TestArchiver_SyncOrCloseFailureKeepsOriginalAndLeavesNoPartialArchive(t *testing.T) {
	for _, tc := range []struct {
		name   string
		break_ func(a *Archiver)
	}{
		{"sync", func(a *Archiver) {
			a.syncFile = func(*os.File) error { return errors.New("injected sync failure") }
		}},
		{"close", func(a *Archiver) {
			a.closeFile = func(f *os.File) error {
				_ = f.Close()
				return errors.New("injected close failure")
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeLogFile(t, dir, "2026-08-01.log", "audit line\n")
			a := NewArchiver(dir, 30)
			a.now = func() time.Time { return time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC) }
			tc.break_(a)

			if err := a.Archive(context.Background()); err == nil {
				t.Fatal("Archive error = nil, want the injected failure")
			}
			got, err := os.ReadFile(filepath.Join(dir, "2026-08-01.log"))
			if err != nil || string(got) != "audit line\n" {
				t.Errorf("original log = %q (err %v), want it kept intact", got, err)
			}
			if _, err := os.Stat(filepath.Join(dir, "2026-08-01.log.gz")); !os.IsNotExist(err) {
				t.Errorf("partial archive left behind (stat err = %v)", err)
			}
		})
	}
}

// The original is removed only after Sync and Close both ran.
func TestArchiver_RemovesOriginalOnlyAfterSyncAndClose(t *testing.T) {
	dir := t.TempDir()
	writeLogFile(t, dir, "2026-08-01.log", "x\n")
	a := NewArchiver(dir, 30)
	a.now = func() time.Time { return time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC) }
	var steps []string
	exists := func() bool { _, err := os.Stat(filepath.Join(dir, "2026-08-01.log")); return err == nil }
	a.syncFile = func(f *os.File) error {
		steps = append(steps, "sync")
		if !exists() {
			t.Error("original removed before Sync")
		}
		return f.Sync()
	}
	a.closeFile = func(f *os.File) error {
		steps = append(steps, "close")
		if !exists() {
			t.Error("original removed before Close")
		}
		return f.Close()
	}
	if err := a.Archive(context.Background()); err != nil {
		t.Fatalf("Archive: %v", err)
	}
	if len(steps) != 2 || steps[0] != "sync" || steps[1] != "close" {
		t.Errorf("steps = %v, want [sync close]", steps)
	}
	if exists() {
		t.Error("original still present after a successful archive")
	}
}
