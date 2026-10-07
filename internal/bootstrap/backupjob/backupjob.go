// Package backupjob is the composition root's daily database backup job:
// the scheduler.DatabaseBackuper whose destination comes from the Settings
// screen's runtime_settings (issue #708, formerly PITHA_BACKUP_DIR).
package backupjob

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/service/backup"
	"github.com/ousiassllc/pitha-trador/internal/service/scheduler/maintenance"
)

// New returns the Backuper of db reading its destination from settings.
func New(db *sql.DB, settings *system.RuntimeSettingsRepository) Backuper {
	return Backuper{db: db, settings: settings}
}

// backupDir returns the backup destination the Settings screen stored
// (runtime_settings config.KeyBackupDir, issue #708), or "" when none is
// configured (the backup is disabled).
func backupDir(ctx context.Context, settings *system.RuntimeSettingsRepository) (string, error) {
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
// check (within ten minutes) without a restart, and clearing it disables
// the backup again. While no directory is configured Backup reports
// maintenance.ErrSkipped: not a failure, and not recorded as done for the
// day. A configured directory that is not available (unmounted drive) is a
// real error, notified by the maintenance runner (backup.Service.Backup).
type Backuper struct {
	db       *sql.DB
	settings *system.RuntimeSettingsRepository
}

func (b Backuper) Backup(ctx context.Context) error {
	dir, err := backupDir(ctx, b.settings)
	if err != nil {
		return err
	}
	if dir == "" {
		return maintenance.ErrSkipped
	}
	return backup.New(b.db, dir, 0).Backup(ctx)
}

// WarnIfDisabled logs the startup WARN for an unset backup directory,
// pointing at the Settings screen that fixes it.
func WarnIfDisabled(ctx context.Context, settings *system.RuntimeSettingsRepository) {
	dir, err := backupDir(ctx, settings)
	if err != nil {
		slog.Error("bootstrap: read backup directory setting", "error", err)
		return
	}
	if dir == "" {
		slog.Warn("bootstrap: daily database backup disabled: no backup directory is configured; set it at /settings (運用設定 > バックアップ先) (requirements/non-functional.md §3)")
	}
}
