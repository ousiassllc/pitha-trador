package backupjob

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap/startup"
	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
)

// dailyCopies is the number of database copies in dir/daily.
func dailyCopies(t *testing.T, dir string) int {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, "daily", "pitha-*.db"))
	if err != nil {
		t.Fatalf("Glob: %v", err)
	}
	return len(matches)
}

func setBackupDir(t *testing.T, settings *system.RuntimeSettingsRepository, dir string) {
	t.Helper()
	if err := settings.Set(context.Background(), config.KeyBackupDir, `"`+filepath.ToSlash(dir)+`"`, time.Now()); err != nil {
		t.Fatalf("Set: %v", err)
	}
}

// Issue #708 / #890: the backup destination comes from the Settings screen's
// runtime_settings row (re-read on every run) and falls back to the default
// same-PC folder; an unavailable configured directory is never replaced by it.
func TestBackuper_FollowsSettingsBackupDirAndFallsBackToDefaultFolder(t *testing.T) {
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	settings := system.NewRuntimeSettingsRepository(db)
	dbPath := filepath.Join(t.TempDir(), "pitha.db")
	defaultDir := startup.DefaultBackupDir(dbPath) // not created yet: the job creates the default
	backuper := New(db, settings, dbPath)
	ctx := context.Background()

	// Unset: a backup is created in the default folder (0700), not skipped.
	if err := backuper.Backup(ctx); err != nil {
		t.Fatalf("Backup with no directory configured: %v", err)
	}
	if got := dailyCopies(t, defaultDir); got != 1 {
		t.Fatalf("daily copies in the default folder = %d, want 1", got)
	}
	if info, err := os.Stat(defaultDir); err != nil || (runtime.GOOS != "windows" && info.Mode().Perm() != 0o700) {
		t.Fatalf("default backup dir stat = %v (err %v), want mode 0700", info, err)
	}

	// Configured: that path is used and the default folder is left alone.
	dest := t.TempDir()
	setBackupDir(t, settings, dest)
	if err := backuper.Backup(ctx); err != nil {
		t.Fatalf("Backup with a configured directory: %v", err)
	}
	if got := dailyCopies(t, dest); got != 1 {
		t.Fatalf("daily copies in %s = %d, want 1", dest, got)
	}

	// A configured but unavailable directory is a real failure: not created,
	// and no silent fallback to the default folder (emptied first to see it).
	if err := os.RemoveAll(defaultDir); err != nil {
		t.Fatalf("RemoveAll: %v", err)
	}
	gone := filepath.Join(dest, "unmounted")
	setBackupDir(t, settings, gone)
	if err := backuper.Backup(ctx); err == nil {
		t.Fatal("Backup into a missing directory succeeded, want a failure")
	}
	if _, statErr := os.Stat(gone); statErr == nil {
		t.Fatal("Backup created the missing destination instead of failing")
	}
	if _, statErr := os.Stat(defaultDir); statErr == nil {
		t.Fatal("a failed configured backup fell back to the default folder")
	}

	// Cleared: back to the default folder, the backup is not disabled.
	if err := settings.Delete(ctx, config.KeyBackupDir); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := backuper.Backup(ctx); err != nil {
		t.Fatalf("Backup after clearing the directory: %v", err)
	}
	if got := dailyCopies(t, defaultDir); got != 1 {
		t.Fatalf("daily copies in the default folder after clearing = %d, want 1", got)
	}
}
