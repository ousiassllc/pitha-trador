// Package backup implements the daily SQLite database backup of
// docs/requirements/non-functional.md §3 "可用性": a consistent copy of the
// database (WAL checkpoint first) into an operator-configured directory
// outside the application's own storage, keeping the last 90 days of
// daily full backups plus 52 weeks of weekly gzip archives.
//
// Layout under the destination directory (which must already exist):
//
//	daily/pitha-YYYY-MM-DD.db      one full copy per day, pruned after 90 days
//	weekly/pitha-YYYY-MM-DD.db.gz  gzip of every Sunday's copy, pruned after 52 weeks
//
// Every copy is scrubbed of the secrets table (credentials are only
// encrypted with the application-embedded key, so they must not leave the
// machine), verified with PRAGMA integrity_check, and written 0600 into
// 0700 directories.
//
// internal/service/scheduler.Scheduler runs Backup once a day through
// scheduler.WithDatabaseBackuper.
package backup

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository"
)

// DefaultRetentionDays is non-functional.md §3's "直近90日分のフルバック
// アップ" window for the daily copies.
const DefaultRetentionDays = 90

// DefaultWeeklyRetentionWeeks bounds how long weekly gzip archives are
// kept (52 weeks), so they do not grow without limit.
const DefaultWeeklyRetentionWeeks = 52

const (
	dailyDir   = "daily"
	weeklyDir  = "weekly"
	filePrefix = "pitha-"
	dayLayout  = "2006-01-02"
	dailySfx   = ".db"
	weeklySfx  = ".db.gz"
	tmpSfx     = ".tmp"

	// dirMode/fileMode keep the copies (which are full databases) private
	// to the owning user.
	dirMode  os.FileMode = 0o700
	fileMode os.FileMode = 0o600
)

// Service backs up one database into a destination directory.
type Service struct {
	db            *sql.DB
	dir           string
	retentionDays int
	weeklyWeeks   int
	now           func() time.Time
}

// New returns a Service copying db's contents into dir. retentionDays
// defaults to DefaultRetentionDays when <= 0.
func New(db *sql.DB, dir string, retentionDays int) *Service {
	if retentionDays <= 0 {
		retentionDays = DefaultRetentionDays
	}
	return &Service{db: db, dir: dir, retentionDays: retentionDays, weeklyWeeks: DefaultWeeklyRetentionWeeks, now: time.Now}
}

// Backup performs one pass: checkpoint + consistent, scrubbed and
// verified copy into daily/, a weekly gzip archive when today is a
// Sunday, then pruning of daily copies older than the retention window
// and weekly archives older than the weekly window. Re-running on the
// same day replaces that day's copy. A failed prune does not undo (or
// mask the success of) the backup itself; it is returned after the copy
// is made.
//
// The destination directory itself is never created: when it does not
// exist (an unmounted external drive or cloud-sync folder) Backup fails
// instead of silently backing up onto the local disk.
func (s *Service) Backup(ctx context.Context) error {
	now := s.now()
	day := now.Format(dayLayout)

	if info, err := os.Stat(s.dir); err != nil {
		return fmt.Errorf("backup: destination %q is not available (external drive not mounted?): %w", s.dir, err)
	} else if !info.IsDir() {
		return fmt.Errorf("backup: destination %q is not a directory", s.dir)
	}

	daily := filepath.Join(s.dir, dailyDir)
	weekly := filepath.Join(s.dir, weeklyDir)
	for _, d := range []string{daily, weekly} {
		if err := os.MkdirAll(d, dirMode); err != nil {
			return fmt.Errorf("backup: create directory %q: %w", d, err)
		}
		// MkdirAll leaves a pre-existing (older, 0755) directory as is.
		if err := os.Chmod(d, dirMode); err != nil {
			return fmt.Errorf("backup: restrict directory %q: %w", d, err)
		}
	}

	dailyPath := filepath.Join(daily, filePrefix+day+dailySfx)
	if err := s.copyDatabase(ctx, dailyPath); err != nil {
		return err
	}

	if now.Weekday() == time.Sunday {
		if err := archiveWeekly(dailyPath, filepath.Join(weekly, filePrefix+day+weeklySfx)); err != nil {
			return err
		}
	}

	return errors.Join(
		prune(ctx, daily, dailySfx, now.AddDate(0, 0, -s.retentionDays)),
		prune(ctx, weekly, weeklySfx, now.AddDate(0, 0, -7*s.weeklyWeeks)),
	)
}

// copyDatabase writes the consistent copy to a temporary 0600 file next
// to dst, scrubs the secrets table, verifies it and renames it into
// place, so a crash never leaves a truncated (or unscrubbed) file under
// a backup's final name.
func (s *Service) copyDatabase(ctx context.Context, dst string) error {
	tmp := dst + tmpSfx
	if err := os.Remove(tmp); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("backup: remove stale temporary file %q: %w", tmp, err)
	}
	// Pre-create the (empty) target with restrictive permissions;
	// VACUUM INTO accepts an empty existing file and keeps its mode.
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, fileMode)
	if err != nil {
		return fmt.Errorf("backup: create temporary file %q: %w", tmp, err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("backup: close temporary file %q: %w", tmp, err)
	}

	busy, err := repository.BackupTo(ctx, s.db, tmp)
	if busy {
		slog.Warn("backup: wal checkpoint could not fully truncate the WAL; the copy is still a consistent snapshot")
	}
	if err == nil {
		err = scrubSecrets(ctx, tmp)
	}
	if err == nil {
		err = verifyCopy(ctx, tmp)
	}
	if err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("backup: copy database: %w", err)
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("backup: finalize %q: %w", dst, err)
	}
	return nil
}
