// Package tempcleanup removes the installer temp directories a finished
// self-update leaves behind (issue #590). It is a leaf package so that both
// internal/service/updater (which creates the directories, DirPattern) and
// cmd/desktop (which cleans them at startup) can use it without a cycle.
package tempcleanup

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"time"
)

// DirPattern is the os.MkdirTemp pattern of the per-download directory
// holding the installer, checksums.txt and its signature (created by
// updater.downloadAndVerify).
const DirPattern = "pitha-trador-update-*"

// CleanupStale removes the temp directories earlier runs left
// behind by downloadAndVerify (the installer, checksums.txt and its
// signature). A successful self-update quits the app and launches the
// installer detached, so nothing is left running that could delete the
// directory; the next startup does it instead. Only directories in
// os.TempDir() named pitha-trador-update-* and last modified before
// startedAt (the current process start time) are removed, so a download
// made by this process is never touched.
//
// Each failure (e.g. Windows refusing to delete the installer that is
// still running) is logged and skipped; the others are still removed and
// the joined error is returned. The caller only logs it: a cleanup failure
// must not block startup. It returns how many directories were removed.
func CleanupStale(startedAt time.Time) (removed int, err error) {
	tmp := os.TempDir()
	entries, err := os.ReadDir(tmp)
	if err != nil {
		return 0, fmt.Errorf("list temp dir: %w", err)
	}

	var errs []error
	for _, entry := range entries {
		// IsDir is false for a symlink, so a link is never followed.
		if !entry.IsDir() {
			continue
		}
		if match, _ := path.Match(DirPattern, entry.Name()); !match {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			errs = append(errs, infoErr)
			continue
		}
		if !info.ModTime().Before(startedAt) {
			continue
		}
		dir := filepath.Join(tmp, entry.Name())
		if rmErr := os.RemoveAll(dir); rmErr != nil {
			slog.Warn("updater: remove stale download dir failed", "dir", dir, "error", rmErr)
			errs = append(errs, rmErr)
			continue
		}
		removed++
	}
	return removed, errors.Join(errs...)
}
