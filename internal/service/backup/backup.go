// Package backup implements the daily SQLite database backup of
// docs/requirements/non-functional.md §3 "可用性": a consistent copy of the
// database (WAL checkpoint first) into an operator-configured directory
// outside the application's own storage, keeping the last 90 days of
// daily full backups plus weekly gzip archives.
//
// Layout under the destination directory:
//
//	daily/pitha-YYYY-MM-DD.db      one full copy per day, pruned after 90 days
//	weekly/pitha-YYYY-MM-DD.db.gz  gzip of every Sunday's copy, kept indefinitely
//
// internal/service/scheduler.Scheduler runs Backup once a day through
// scheduler.WithDatabaseBackuper.
package backup

import (
	"compress/gzip"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository"
)

// DefaultRetentionDays is non-functional.md §3's "直近90日分のフルバック
// アップ" window for the daily copies.
const DefaultRetentionDays = 90

const (
	dailyDir   = "daily"
	weeklyDir  = "weekly"
	filePrefix = "pitha-"
	dayLayout  = "2006-01-02"
	dailySfx   = ".db"
	weeklySfx  = ".db.gz"
	tmpSfx     = ".tmp"
)

// Service backs up one database into a destination directory.
type Service struct {
	db            *sql.DB
	dir           string
	retentionDays int
	now           func() time.Time
}

// New returns a Service copying db's contents into dir. retentionDays
// defaults to DefaultRetentionDays when <= 0.
func New(db *sql.DB, dir string, retentionDays int) *Service {
	if retentionDays <= 0 {
		retentionDays = DefaultRetentionDays
	}
	return &Service{db: db, dir: dir, retentionDays: retentionDays, now: time.Now}
}

// Backup performs one pass: checkpoint + consistent copy into
// daily/, a weekly gzip archive when today is a Sunday, then pruning of
// daily copies older than the retention window. Re-running on the same
// day replaces that day's copy. A failed prune does not undo (or mask
// the success of) the backup itself; it is returned after the copy is
// made.
func (s *Service) Backup(ctx context.Context) error {
	now := s.now()
	day := now.Format(dayLayout)

	daily := filepath.Join(s.dir, dailyDir)
	weekly := filepath.Join(s.dir, weeklyDir)
	for _, d := range []string{daily, weekly} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return fmt.Errorf("backup: create directory %q: %w", d, err)
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

	return s.prune(ctx, daily, now)
}

// copyDatabase writes the consistent copy to a temporary file next to
// dst and renames it into place, so a crash never leaves a truncated
// file under a backup's final name.
func (s *Service) copyDatabase(ctx context.Context, dst string) error {
	tmp := dst + tmpSfx
	if err := os.Remove(tmp); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("backup: remove stale temporary file %q: %w", tmp, err)
	}
	busy, err := repository.BackupTo(ctx, s.db, tmp)
	if busy {
		slog.Warn("backup: wal checkpoint could not fully truncate the WAL; the copy is still a consistent snapshot")
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

// archiveWeekly gzips src into dst (via a temporary file + rename).
func archiveWeekly(src, dst string) (err error) {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("backup: open %q for weekly archive: %w", src, err)
	}
	defer func() { _ = in.Close() }()

	tmp := dst + tmpSfx
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("backup: create weekly archive %q: %w", tmp, err)
	}
	defer func() {
		if err != nil {
			_ = os.Remove(tmp)
		}
	}()

	gz := gzip.NewWriter(out)
	if _, err = io.Copy(gz, in); err != nil {
		_ = gz.Close()
		_ = out.Close()
		return fmt.Errorf("backup: compress %q: %w", src, err)
	}
	if err = gz.Close(); err != nil {
		_ = out.Close()
		return fmt.Errorf("backup: finalize weekly archive %q: %w", tmp, err)
	}
	if err = out.Close(); err != nil {
		return fmt.Errorf("backup: close weekly archive %q: %w", tmp, err)
	}
	if err = os.Rename(tmp, dst); err != nil {
		return fmt.Errorf("backup: finalize weekly archive %q: %w", dst, err)
	}
	return nil
}

// prune removes daily copies whose date is more than retentionDays
// before now. Entries that are not pitha-YYYY-MM-DD.db files are left
// alone.
func (s *Service) prune(ctx context.Context, dir string, now time.Time) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("backup: read %q: %w", dir, err)
	}
	cutoff := now.AddDate(0, 0, -s.retentionDays).Format(dayLayout)
	var errs []error
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return errors.Join(append(errs, err)...)
		}
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, filePrefix) || !strings.HasSuffix(name, dailySfx) {
			continue
		}
		day := strings.TrimSuffix(strings.TrimPrefix(name, filePrefix), dailySfx)
		if _, err := time.Parse(dayLayout, day); err != nil {
			continue
		}
		// ISO dates order lexicographically.
		if day >= cutoff {
			continue
		}
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			errs = append(errs, fmt.Errorf("backup: prune %q: %w", name, err))
		}
	}
	return errors.Join(errs...)
}
