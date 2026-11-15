// Package backupjob is the composition root's daily database backup job:
// the scheduler.DatabaseBackuper whose destination comes from the Settings
// screen's runtime_settings (issue #708, formerly PITHA_BACKUP_DIR) and
// falls back to a default folder on the same PC (issue #890).
package backupjob

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap/startup"
	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/service/backup"
)

// defaultDirMode is the mode of the default backup folder it is created with.
const defaultDirMode os.FileMode = 0o700

// New returns the Backuper of db reading its destination from settings and
// falling back to startup.DefaultBackupDir of the database at dbPath (a
// folder on the same PC) while none is configured.
func New(db *sql.DB, settings *system.RuntimeSettingsRepository, dbPath string) Backuper {
	return Backuper{db: db, settings: settings, defaultDir: startup.DefaultBackupDir(dbPath)}
}

// configuredDir returns the backup destination the Settings screen stored
// (runtime_settings config.KeyBackupDir, issue #708), or "" when none is
// configured (the default folder applies).
func configuredDir(ctx context.Context, settings *system.RuntimeSettingsRepository) (string, error) {
	raw, ok, err := settings.Get(ctx, config.KeyBackupDir)
	if err != nil || !ok {
		return "", err
	}
	var dir string
	if err := json.Unmarshal([]byte(raw), &dir); err != nil {
		return "", fmt.Errorf("bootstrap: stored %s=%q is not a JSON string: %w", config.KeyBackupDir, raw, err)
	}
	return strings.TrimSpace(dir), nil
}

// Backuper is the scheduler.DatabaseBackuper of the daily database
// backup. It re-reads the destination from runtime_settings on every run,
// so a directory saved in Settings takes effect on the next maintenance
// check (within ten minutes) without a restart, and clearing it returns to
// the default folder (the backup is never disabled). Only the default folder
// is created when missing; a configured directory that is not available
// (unmounted drive) is a real error, notified by the maintenance runner
// (backup.Service.Backup), and never replaced by the default.
type Backuper struct {
	db         *sql.DB
	settings   *system.RuntimeSettingsRepository
	defaultDir string
}

func (b Backuper) Backup(ctx context.Context) error {
	dir, err := configuredDir(ctx, b.settings)
	if err != nil {
		return err
	}
	if dir == "" {
		dir = b.defaultDir
		if err := os.MkdirAll(dir, defaultDirMode); err != nil {
			return fmt.Errorf("bootstrap: create default backup directory %q: %w", dir, err)
		}
	}
	return backup.New(b.db, dir, 0).Backup(ctx)
}

// LogDestination logs the effective backup destination at startup (INFO)
// and, when it is the default folder on the same PC, one WARN that it does
// not survive a disk failure. No secrets.
func LogDestination(ctx context.Context, settings *system.RuntimeSettingsRepository, dbPath string) {
	dir, err := configuredDir(ctx, settings)
	if err != nil {
		slog.Error("bootstrap: read backup directory setting", "error", err)
		return
	}
	if dir != "" {
		slog.Info("bootstrap: daily database backup destination", "dir", dir, "default", false)
		return
	}
	slog.Info("bootstrap: daily database backup destination", "dir", startup.DefaultBackupDir(dbPath), "default", true)
	slog.Warn("bootstrap: daily database backup uses the default folder on this PC, which does not survive a disk failure; choose an external path at /settings (運用設定 > バックアップ先)")
}
