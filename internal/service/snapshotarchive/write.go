package snapshotarchive

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
)

// selectPageSQL reads one page of date's rows after the keyset (timestamp,
// id), in the archive's order. Served by market_snapshots_timestamp_idx.
const selectPageSQL = `SELECT * FROM market_snapshots
	WHERE timestamp >= ? AND timestamp < ? AND id <= ?
	  AND (timestamp > ? OR (timestamp = ? AND id > ?))
	ORDER BY timestamp, id LIMIT ?`

// writeDayFile writes date's archive file and returns what the re-read file
// holds. With no previous file (prev.Rows == 0) it holds exactly the
// database's rows (id <= maxID) of the date. With one, it holds the union:
// the previous file's rows plus the database rows the file lacks, so rows
// already purged from the database are never lost from the archive.
//
// The rows go to a temporary file first; it is renamed to the final name
// only after it is fsynced, re-read and verified against the database and the
// previous file (verifyWritten), so no unverified or partial file ever has
// the final name.
func (a *Archiver) writeDayFile(ctx context.Context, date string, maxID int64, dbd dbDay, prev fileInfo) (fileInfo, error) {
	next, err := nextDate(date)
	if err != nil {
		return fileInfo{}, err
	}
	final := a.filePath(date)
	w, err := newFileWriter(filepath.Dir(final))
	if err != nil {
		return fileInfo{}, fmt.Errorf("create archive file: %w", err)
	}
	tmp := w.f.Name()
	if prev.Rows == 0 {
		err = a.scanDay(ctx, date, next, maxID, func(rec []string) error { return w.write(rec) })
	} else {
		err = a.writeUnion(ctx, w, date, next, maxID, prev)
	}
	if err == nil {
		err = w.finish()
	}
	if err != nil {
		w.abort()
		return fileInfo{}, fmt.Errorf("write archive file: %w", err)
	}
	info, err := scanFile(tmp, date)
	if err == nil {
		err = verifyWritten(info, dbd, prev)
	}
	if err != nil {
		_ = os.Remove(tmp)
		return fileInfo{}, fmt.Errorf("verify archive file: %w", err)
	}
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Remove(tmp)
		return fileInfo{}, fmt.Errorf("publish archive file: %w", err)
	}
	if err := syncDir(filepath.Dir(final)); err != nil {
		return fileInfo{}, fmt.Errorf("sync archive folder: %w", err)
	}
	return info, nil
}

// verifyWritten checks a freshly written file against the database (dbd) and
// the file it replaces (prev, zero when none): it holds every row of both,
// nothing else, spans exactly their combined timestamp range, and so is never
// smaller than prev.
func verifyWritten(info fileInfo, dbd dbDay, prev fileInfo) error {
	_, extra := compareIDs(dbd.IDs, prev.IDs)
	if want := len(dbd.IDs) + extra; info.Rows != want {
		return fmt.Errorf("archive has %d rows, want %d (database %d, previous archive only %d)", info.Rows, want, len(dbd.IDs), extra)
	}
	if missing, _ := compareIDs(dbd.IDs, info.IDs); missing != 0 {
		return fmt.Errorf("archive lacks %d database rows", missing)
	}
	for id := range prev.IDs {
		if _, ok := info.IDs[id]; !ok {
			return fmt.Errorf("archive lost row %d of the previous archive", id)
		}
	}
	first, last := dbd.FirstTimestamp, dbd.LastTimestamp
	if prev.Rows > 0 {
		first, last = min(first, prev.FirstTimestamp), max(last, prev.LastTimestamp)
	}
	if info.FirstTimestamp != first || info.LastTimestamp != last {
		return fmt.Errorf("archive range %s..%s differs from the expected range %s..%s",
			info.FirstTimestamp, info.LastTimestamp, first, last)
	}
	return nil
}

// scanDay streams date's database rows (id <= maxID), in (timestamp, id)
// order, page by page: emit gets the column names first and then every row
// as CSV fields (the slice is reused between calls).
func (a *Archiver) scanDay(ctx context.Context, date, next string, maxID int64, emit func(rec []string) error) error {
	var lastTS string
	var lastID int64
	wroteHeader := false
	for {
		rows, err := a.db.QueryContext(ctx, selectPageSQL,
			date, next, maxID, lastTS, lastTS, lastID, a.pageSize)
		if err != nil {
			return err
		}
		n, ts, id, err := scanPage(rows, emit, &wroteHeader)
		if err != nil {
			return err
		}
		if n < a.pageSize {
			return nil
		}
		lastTS, lastID = ts, id
	}
}

// scanPage emits every row of rows (closing it) and returns how many, and
// the (timestamp, id) of the last.
func scanPage(rows *sql.Rows, emit func(rec []string) error, wroteHeader *bool) (n int, lastTS string, lastID int64, err error) {
	defer func() { _ = rows.Close() }()
	cols, err := rows.Columns()
	if err != nil {
		return 0, "", 0, err
	}
	idCol, tsCol := indexOf(cols, "id"), indexOf(cols, "timestamp")
	if idCol < 0 || tsCol < 0 {
		return 0, "", 0, fmt.Errorf("%s has no id/timestamp column", Table)
	}
	if !*wroteHeader {
		if err := emit(cols); err != nil {
			return 0, "", 0, err
		}
		*wroteHeader = true
	}
	vals := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	rec := make([]string, len(cols))
	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			return 0, "", 0, err
		}
		for i, v := range vals {
			if rec[i], err = formatValue(v); err != nil {
				return 0, "", 0, fmt.Errorf("column %s: %w", cols[i], err)
			}
		}
		if err := emit(rec); err != nil {
			return 0, "", 0, err
		}
		n++
		lastTS = rec[tsCol]
		if lastID, err = strconv.ParseInt(rec[idCol], 10, 64); err != nil {
			return 0, "", 0, err
		}
	}
	return n, lastTS, lastID, rows.Err()
}

// writeUnion writes the previous file's rows as they are, then the database
// rows (id <= maxID) the previous file lacks. The database's columns must be
// the file's: a table changed since the file was written is an error.
func (a *Archiver) writeUnion(ctx context.Context, w *fileWriter, date, next string, maxID int64, prev fileInfo) error {
	if _, err := scanFileWith(a.filePath(date), date, func(rec []string) error { return w.write(rec) }); err != nil {
		return err
	}
	first := true
	return a.scanDay(ctx, date, next, maxID, func(rec []string) error {
		if first {
			first = false
			if !slices.Equal(rec, prev.Header) {
				return fmt.Errorf("columns of %s changed since the archive file was written", Table)
			}
			return nil
		}
		id, err := strconv.ParseInt(rec[slices.Index(prev.Header, "id")], 10, 64)
		if err != nil {
			return err
		}
		if _, ok := prev.IDs[id]; ok {
			return nil
		}
		return w.write(rec)
	})
}
