package logging

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const (
	// MaxExportBytes is the total size cap of one Export result
	// (requirements/functional/components-platform.md FR-ERRLOG-4).
	MaxExportBytes = 10 << 20
	// maxExportLineBytes is the longest log line Export will consider; a
	// longer line is skipped (FR-ERRLOG-2).
	maxExportLineBytes = 1 << 20

	// redacted replaces every masked value (FR-ERRLOG-3).
	redacted = "[REDACTED]"
)

// ExportResult is one Export's outcome: Data is the NDJSON body (one masked
// slog record per line, oldest first), Records its line count, and
// Truncated reports that older records were dropped to fit MaxExportBytes.
type ExportResult struct {
	Data      []byte
	Records   int
	Truncated bool
}

// Exporter extracts level-filtered, secret-masked records from the daily log
// files RotatingWriter writes and Archiver compresses
// (docs/architecture/overview/flows.md §10.6, FR-ERRLOG-1〜7). It only reads
// dir and is independent of both writers, so it can run while they do.
type Exporter struct {
	dir      string
	maxBytes int
	now      func() time.Time
}

// NewExporter returns an Exporter over dir (the RotatingWriter's directory).
func NewExporter(dir string) *Exporter {
	return &Exporter{dir: dir, maxBytes: MaxExportBytes, now: time.Now}
}

// exportedRecord is one kept log line with the time used to order it.
type exportedRecord struct {
	at   time.Time
	line []byte // masked record, newline-terminated
}

// Export returns the records of the last days UTC calendar days (today
// included, same basis as the file names) whose level is at least minLevel,
// in time order. A day with neither <day>.log nor <day>.log.gz contributes
// nothing. When the result would exceed MaxExportBytes the oldest records
// are dropped and Truncated is set. An unreadable log directory or file is
// an error.
func (e *Exporter) Export(ctx context.Context, days int, minLevel slog.Level) (ExportResult, error) {
	if days < 1 {
		return ExportResult{}, fmt.Errorf("logging: export days must be >= 1, got %d", days)
	}
	if info, err := os.Stat(e.dir); err != nil {
		return ExportResult{}, fmt.Errorf("logging: read log directory %q: %w", e.dir, err)
	} else if !info.IsDir() {
		return ExportResult{}, fmt.Errorf("logging: log directory %q is not a directory", e.dir)
	}

	today := e.now().UTC()
	var (
		perDay    [][]exportedRecord // newest day first
		remaining = e.maxBytes
		truncated bool
	)
	// Newest day first so the byte budget is spent on the newest records.
	for i := range days {
		if err := ctx.Err(); err != nil {
			return ExportResult{}, err
		}
		day := today.AddDate(0, 0, -i).Format(dailyFileLayout)
		records, dropped, err := e.collectDay(day, minLevel, remaining)
		if err != nil {
			return ExportResult{}, err
		}
		truncated = truncated || dropped
		for _, r := range records {
			remaining -= len(r.line)
		}
		perDay = append(perDay, records)
	}

	var all []exportedRecord
	for i := len(perDay) - 1; i >= 0; i-- { // oldest day first
		all = append(all, perDay[i]...)
	}
	// Files are chronological, but a record is filed by write time rather
	// than record time, so adjacent days can overlap slightly.
	sort.SliceStable(all, func(i, j int) bool { return all[i].at.Before(all[j].at) })

	var buf bytes.Buffer
	buf.Grow(e.maxBytes - remaining)
	for _, r := range all {
		buf.Write(r.line)
	}
	return ExportResult{Data: buf.Bytes(), Records: len(all), Truncated: truncated}, nil
}

// collectDay reads one day's file (plain if present, else gzip archive) and
// returns its matching records, at most budget bytes of them: when more
// match, the oldest are dropped (dropped = true).
func (e *Exporter) collectDay(day string, minLevel slog.Level, budget int) (records []exportedRecord, dropped bool, err error) {
	r, err := e.openDay(day)
	if err != nil {
		return nil, false, err
	}
	if r == nil {
		return nil, false, nil
	}
	defer func() { _ = r.Close() }()

	var size int
	last := time.Time{}
	err = readCompleteLines(r, func(line []byte) {
		rec, ok := maskedRecord(line, minLevel)
		if !ok {
			return
		}
		if rec.at.IsZero() {
			rec.at = last
		}
		last = rec.at
		records = append(records, rec)
		size += len(rec.line)
		for size > budget && len(records) > 0 {
			size -= len(records[0].line)
			records[0] = exportedRecord{}
			records = records[1:]
			dropped = true
		}
	})
	if err != nil {
		return nil, false, fmt.Errorf("logging: read log for %s: %w", day, err)
	}
	return records, dropped, nil
}

// openDay opens dir/<day>.log, falling back to dir/<day>.log.gz (also when
// the Archiver removes the plain file between the two attempts). It returns
// (nil, nil) when the day has no file.
func (e *Exporter) openDay(day string) (io.ReadCloser, error) {
	path := filepath.Join(e.dir, day+".log")
	f, err := os.Open(path)
	if err == nil {
		return f, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("logging: open %q: %w", path, err)
	}
	gzPath := path + ".gz"
	f, err = os.Open(gzPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("logging: open %q: %w", gzPath, err)
	}
	gz, err := gzip.NewReader(f)
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("logging: open archive %q: %w", gzPath, err)
	}
	return &gzipFile{Reader: gz, file: f}, nil
}

// gzipFile closes the underlying file along with the gzip reader.
type gzipFile struct {
	*gzip.Reader
	file *os.File
}

func (g *gzipFile) Close() error {
	err := g.Reader.Close()
	if cerr := g.file.Close(); err == nil {
		err = cerr
	}
	return err
}

// readCompleteLines calls fn with every newline-terminated line of r
// (terminator removed) that is at most maxExportLineBytes long. A trailing
// line without a newline is dropped: in a file still being written it may be
// cut mid-record (FR-ERRLOG-6).
func readCompleteLines(r io.Reader, fn func(line []byte)) error {
	br := bufio.NewReaderSize(r, 64<<10)
	var line []byte
	skipping := false
	for {
		frag, err := br.ReadSlice('\n')
		if !skipping {
			line = append(line, frag...)
			if len(line) > maxExportLineBytes+2 { // +2: \r\n
				skipping = true
				line = nil
			}
		}
		switch {
		case err == nil:
			if !skipping {
				if trimmed := bytes.TrimRight(line, "\r\n"); len(trimmed) <= maxExportLineBytes {
					fn(trimmed)
				}
			}
			line, skipping = line[:0], false
		case errors.Is(err, bufio.ErrBufferFull):
		case errors.Is(err, io.EOF):
			return nil
		default:
			return err
		}
	}
}

// maskedRecord returns line masked for sharing and its record time when line
// is a JSON object whose "level" is at least minLevel.
func maskedRecord(line []byte, minLevel slog.Level) (exportedRecord, bool) {
	var head struct {
		Time  string `json:"time"`
		Level string `json:"level"`
	}
	if json.Unmarshal(line, &head) != nil || head.Level == "" {
		return exportedRecord{}, false
	}
	var level slog.Level
	if level.UnmarshalText([]byte(head.Level)) != nil || level < minLevel {
		return exportedRecord{}, false
	}
	masked, err := maskJSON(line)
	if err != nil {
		return exportedRecord{}, false
	}
	at, _ := time.Parse(time.RFC3339Nano, head.Time)
	return exportedRecord{at: at, line: append(masked, '\n')}, true
}
