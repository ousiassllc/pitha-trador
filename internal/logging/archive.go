package logging

import (
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DefaultRetentionDays is non-functional.md §5's local-retention window:
// "直近30日はローカル保持、それ以前は圧縮アーカイブする".
const DefaultRetentionDays = 30

// Archiver gzip-compresses (and removes the plaintext original of) every
// dir/<YYYY-MM-DD>.log file - RotatingWriter's own naming convention -
// whose date is older than RetentionDays, leaving a
// "<YYYY-MM-DD>.log.gz" file in its place.
// internal/bootstrap wires an Archiver over the directory a RotatingWriter
// writes to via scheduler.WithLogRotator; the Scheduler then runs Rotate as
// the "log_rotation" task of its maintenance.Runner (once per day, on start
// and on every 10-minute catch-up tick until it has succeeded today).
type Archiver struct {
	dir           string
	retentionDays int
	now           func() time.Time
}

// NewArchiver returns an Archiver over dir. retentionDays defaults to
// DefaultRetentionDays when <= 0.
func NewArchiver(dir string, retentionDays int) *Archiver {
	if retentionDays <= 0 {
		retentionDays = DefaultRetentionDays
	}
	return &Archiver{dir: dir, retentionDays: retentionDays, now: time.Now}
}

// Rotate implements internal/service/scheduler.LogRotator by delegating
// to Archive.
func (a *Archiver) Rotate(ctx context.Context) error {
	return a.Archive(ctx)
}

// Archive performs one archival pass (see the Archiver doc comment). A
// dir entry whose name is not a "<YYYY-MM-DD>.log" RotatingWriter file
// (a directory, an already-archived ".log.gz", or anything else) is left
// untouched.
func (a *Archiver) Archive(ctx context.Context) error {
	entries, err := os.ReadDir(a.dir)
	if err != nil {
		return fmt.Errorf("logging: read log directory %q: %w", a.dir, err)
	}

	cutoff := a.now().UTC().AddDate(0, 0, -a.retentionDays)
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".log") {
			continue
		}
		day, err := time.Parse(dailyFileLayout, strings.TrimSuffix(entry.Name(), ".log"))
		if err != nil {
			continue
		}
		if !day.Before(cutoff) {
			continue
		}
		if err := a.compress(entry.Name()); err != nil {
			return err
		}
	}
	return nil
}

// compress gzips dir/name to dir/name.gz and removes dir/name once the
// archive is durably written.
func (a *Archiver) compress(name string) (err error) {
	srcPath := filepath.Join(a.dir, name)
	dstPath := srcPath + ".gz"

	src, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("logging: open %q for archival: %w", srcPath, err)
	}
	defer func() { _ = src.Close() }()

	dst, err := os.OpenFile(dstPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, logFileMode)
	if err != nil {
		return fmt.Errorf("logging: create archive %q: %w", dstPath, err)
	}
	defer func() {
		if cerr := dst.Close(); err == nil {
			err = cerr
		}
	}()

	gz := gzip.NewWriter(dst)
	if _, copyErr := io.Copy(gz, src); copyErr != nil {
		_ = gz.Close()
		_ = os.Remove(dstPath)
		return fmt.Errorf("logging: compress %q: %w", srcPath, copyErr)
	}
	if closeErr := gz.Close(); closeErr != nil {
		_ = os.Remove(dstPath)
		return fmt.Errorf("logging: finalize archive %q: %w", dstPath, closeErr)
	}

	if rmErr := os.Remove(srcPath); rmErr != nil {
		return fmt.Errorf("logging: remove archived source %q: %w", srcPath, rmErr)
	}
	return nil
}
