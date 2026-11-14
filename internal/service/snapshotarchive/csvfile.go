package snapshotarchive

import (
	"compress/gzip"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Archive file format: gzip-compressed CSV (RFC 4180, encoding/csv). The
// first record is the header (the table's column names); a NULL is an empty
// field (every nullable column is numeric, so NULL and "" never collide on
// a column that can hold text but not NULL, e.g. price_limit); numbers are
// written in their shortest exact decimal form. CSV loads directly into
// pandas / DuckDB / R without a custom reader. The gzip header carries no
// timestamp or file name, so the same rows always compress to the same
// bytes.

// fileInfo is what scanning an archive file found.
type fileInfo struct {
	Header         []string
	Rows           int
	IDs            map[int64]struct{}
	FirstTimestamp string
	LastTimestamp  string
	SHA256         string
}

// formatValue renders one scanned database value as a CSV field.
func formatValue(v any) (string, error) {
	switch x := v.(type) {
	case nil:
		return "", nil
	case int64:
		return strconv.FormatInt(x, 10), nil
	case float64:
		return strconv.FormatFloat(x, 'g', -1, 64), nil
	case bool:
		if x {
			return "1", nil
		}
		return "0", nil
	case string:
		return x, nil
	case []byte:
		return string(x), nil
	case time.Time:
		return x.UTC().Format(time.RFC3339Nano), nil
	default:
		return "", fmt.Errorf("unsupported column value type %T", v)
	}
}

// fileWriter streams records into a temporary file as gzip-compressed CSV.
type fileWriter struct {
	f   *os.File
	gz  *gzip.Writer
	csv *csv.Writer
}

// newFileWriter creates the temporary file in dir (mode 0600).
func newFileWriter(dir string) (*fileWriter, error) {
	f, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return nil, err
	}
	gz := gzip.NewWriter(f)
	return &fileWriter{f: f, gz: gz, csv: csv.NewWriter(gz)}, nil
}

func (w *fileWriter) write(record []string) error { return w.csv.Write(record) }

// finish flushes, fsyncs and closes the file, so its bytes are on disk
// before the caller renames it.
func (w *fileWriter) finish() error {
	w.csv.Flush()
	if err := w.csv.Error(); err != nil {
		return err
	}
	if err := w.gz.Close(); err != nil {
		return err
	}
	if err := w.f.Sync(); err != nil {
		return err
	}
	return w.f.Close()
}

// abort closes and removes the temporary file after a failure.
func (w *fileWriter) abort() {
	_ = w.f.Close()
	_ = os.Remove(w.f.Name())
}

// scanFile reads the whole archive file at path: it checks the gzip
// checksum, the header (id and timestamp columns), a constant field count,
// that every timestamp belongs to date and that ids are unique, and returns
// the row count, id set, timestamp range and the sha256 of the file's
// bytes. A missing file returns an error satisfying errors.Is(err,
// os.ErrNotExist).
func scanFile(path, date string) (fileInfo, error) { return scanFileWith(path, date, nil) }

// scanFileWith is scanFile that also hands the header and then every
// validated row to emit (when non-nil); emit must not keep the record.
func scanFileWith(path, date string, emit func(rec []string) error) (fileInfo, error) {
	f, err := os.Open(path)
	if err != nil {
		return fileInfo{}, err
	}
	defer func() { _ = f.Close() }()
	hash := sha256.New()
	tee := io.TeeReader(f, hash)
	gz, err := gzip.NewReader(tee)
	if err != nil {
		return fileInfo{}, fmt.Errorf("%s: not a gzip file: %w", path, err)
	}
	r := csv.NewReader(gz)
	r.ReuseRecord = true
	header, err := r.Read()
	if err != nil {
		return fileInfo{}, fmt.Errorf("%s: read header: %w", path, err)
	}
	idCol, tsCol := indexOf(header, "id"), indexOf(header, "timestamp")
	if idCol < 0 || tsCol < 0 {
		return fileInfo{}, fmt.Errorf("%s: header has no id/timestamp column", path)
	}
	if emit != nil {
		if err := emit(header); err != nil {
			return fileInfo{}, err
		}
	}
	info := fileInfo{Header: slices.Clone(header), IDs: map[int64]struct{}{}}
	for {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fileInfo{}, fmt.Errorf("%s: row %d: %w", path, info.Rows+1, err)
		}
		id, err := strconv.ParseInt(rec[idCol], 10, 64)
		if err != nil {
			return fileInfo{}, fmt.Errorf("%s: row %d: bad id: %w", path, info.Rows+1, err)
		}
		if _, dup := info.IDs[id]; dup {
			return fileInfo{}, fmt.Errorf("%s: row %d: duplicate id %d", path, info.Rows+1, id)
		}
		ts := rec[tsCol]
		if !strings.HasPrefix(ts, date) {
			return fileInfo{}, fmt.Errorf("%s: row %d: timestamp %q is outside %s", path, info.Rows+1, ts, date)
		}
		if emit != nil {
			if err := emit(rec); err != nil {
				return fileInfo{}, err
			}
		}
		info.IDs[id] = struct{}{}
		if info.Rows == 0 || ts < info.FirstTimestamp {
			info.FirstTimestamp = ts
		}
		if ts > info.LastTimestamp {
			info.LastTimestamp = ts
		}
		info.Rows++
	}
	// Drain what the gzip reader left unread so the hash covers the file.
	if _, err := io.Copy(io.Discard, tee); err != nil {
		return fileInfo{}, fmt.Errorf("%s: %w", path, err)
	}
	info.SHA256 = hex.EncodeToString(hash.Sum(nil))
	return info, nil
}

func indexOf(header []string, name string) int {
	for i, h := range header {
		if h == name {
			return i
		}
	}
	return -1
}
