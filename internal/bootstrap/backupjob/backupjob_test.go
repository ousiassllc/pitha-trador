package backupjob

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/service/scheduler/maintenance"
)

// Issue #708: the backup destination comes from the Settings screen's
// runtime_settings row, re-read on every run.
func TestSettingsBackuper_FollowsSettingsBackupDir(t *testing.T) {
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	settings := system.NewRuntimeSettingsRepository(db)
	backuper := New(db, settings)
	ctx := context.Background()

	if err := backuper.Backup(ctx); !errors.Is(err, maintenance.ErrSkipped) {
		t.Fatalf("Backup with no directory configured = %v, want maintenance.ErrSkipped", err)
	}

	dest := t.TempDir()
	if err := settings.Set(ctx, config.KeyBackupDir, `"`+filepath.ToSlash(dest)+`"`, time.Now()); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := backuper.Backup(ctx); err != nil {
		t.Fatalf("Backup with a configured directory: %v", err)
	}
	matches, err := filepath.Glob(filepath.Join(dest, "daily", "pitha-*.db"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("daily copies in %s = %v (err %v), want exactly one", dest, matches, err)
	}

	// A configured but unavailable directory is a real failure, not a skip.
	gone := filepath.Join(dest, "unmounted")
	if err := settings.Set(ctx, config.KeyBackupDir, `"`+filepath.ToSlash(gone)+`"`, time.Now()); err != nil {
		t.Fatalf("Set: %v", err)
	}
	err = backuper.Backup(ctx)
	if err == nil || errors.Is(err, maintenance.ErrSkipped) {
		t.Fatalf("Backup into a missing directory = %v, want a failure", err)
	}
	if _, statErr := os.Stat(gone); statErr == nil {
		t.Fatal("Backup created the missing destination instead of failing")
	}

	if err := settings.Delete(ctx, config.KeyBackupDir); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := backuper.Backup(ctx); !errors.Is(err, maintenance.ErrSkipped) {
		t.Fatalf("Backup after clearing the directory = %v, want maintenance.ErrSkipped", err)
	}
}
