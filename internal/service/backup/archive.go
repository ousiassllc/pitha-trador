package backup

import (
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// archiveWeekly gzips src into dst (via a temporary file + rename). The
// archive is created 0600 like every other file under the destination.
func archiveWeekly(src, dst string) (err error) {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("backup: open %q for weekly archive: %w", src, err)
	}
	defer func() { _ = in.Close() }()

	tmp := dst + tmpSfx
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, fileMode)
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

// prune removes pitha-YYYY-MM-DD<suffix> files in dir whose date is
// before cutoff. Entries that do not match that pattern are left alone.
func prune(ctx context.Context, dir, suffix string, cutoff time.Time) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("backup: read %q: %w", dir, err)
	}
	cutoffDay := cutoff.Format(dayLayout)
	var errs []error
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return errors.Join(append(errs, err)...)
		}
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, filePrefix) || !strings.HasSuffix(name, suffix) {
			continue
		}
		day := strings.TrimSuffix(strings.TrimPrefix(name, filePrefix), suffix)
		if _, err := time.Parse(dayLayout, day); err != nil {
			continue
		}
		// ISO dates order lexicographically.
		if day >= cutoffDay {
			continue
		}
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			errs = append(errs, fmt.Errorf("backup: prune %q: %w", name, err))
		}
	}
	return errors.Join(errs...)
}

// isoWeekMonday returns the (midnight, same location) Monday of t's ISO
// week, the key weekly archives are named and de-duplicated by.
func isoWeekMonday(t time.Time) time.Time {
	daysSinceMonday := (int(t.Weekday()) + 6) % 7
	return time.Date(t.Year(), t.Month(), t.Day()-daysSinceMonday, 0, 0, 0, 0, t.Location())
}

// hasWeeklyArchive reports whether dir already holds a weekly archive dated
// within the ISO week starting at monday (any day of that week, so archives
// written under the earlier Sunday-only naming still count).
func hasWeeklyArchive(dir string, monday time.Time) (bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, fmt.Errorf("backup: read %q: %w", dir, err)
	}
	first, next := monday.Format(dayLayout), monday.AddDate(0, 0, 7).Format(dayLayout)
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, filePrefix) || !strings.HasSuffix(name, weeklySfx) {
			continue
		}
		day := strings.TrimSuffix(strings.TrimPrefix(name, filePrefix), weeklySfx)
		if _, err := time.Parse(dayLayout, day); err != nil {
			continue
		}
		// ISO dates order lexicographically.
		if day >= first && day < next {
			return true, nil
		}
	}
	return false, nil
}
