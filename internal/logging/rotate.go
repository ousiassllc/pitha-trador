package logging

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// dailyFileLayout is RotatingWriter's log file naming convention
// (also Archiver's own parsing format, archive.go).
const dailyFileLayout = "2006-01-02"

// RotatingWriter is an io.Writer that writes into dir/<YYYY-MM-DD>.log,
// switching to a new file the moment the UTC calendar date changes
// (non-functional.md §5 "ログは日次ローテーションし"). It is safe for
// concurrent use.
type RotatingWriter struct {
	dir string
	now func() time.Time

	mu   sync.Mutex
	day  string
	file *os.File
}

// NewRotatingWriter returns a RotatingWriter writing into dir, creating
// dir (and any missing parents) if it does not already exist.
func NewRotatingWriter(dir string) (*RotatingWriter, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("logging: create log directory %q: %w", dir, err)
	}
	return &RotatingWriter{dir: dir, now: time.Now}, nil
}

// Write implements io.Writer, opening (or rolling over to) today's log
// file first if the date has changed since the last Write.
func (w *RotatingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	day := w.now().UTC().Format(dailyFileLayout)
	if w.file == nil || day != w.day {
		if err := w.rotateLocked(day); err != nil {
			return 0, err
		}
	}
	return w.file.Write(p)
}

func (w *RotatingWriter) rotateLocked(day string) error {
	if w.file != nil {
		_ = w.file.Close()
	}
	f, err := os.OpenFile(filepath.Join(w.dir, day+".log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("logging: open log file for %s: %w", day, err)
	}
	w.file = f
	w.day = day
	return nil
}

// Close closes the currently open log file, if any.
func (w *RotatingWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
}
