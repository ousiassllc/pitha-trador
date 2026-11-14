// Package snapshotarchive copies market_snapshots rows to compressed
// per-day files on the local disk before the retention purge deletes them
// (issue #1072, docs/requirements/non-functional.md §3 "データ保持"), so the
// 1-minute market data stays available for research after the 90-day
// window.
//
// Layout under the archive directory (DefaultDir, a folder next to the
// database):
//
//	market_snapshots/manifest.json
//	market_snapshots/2026/market_snapshots-2026-07-01.csv.gz
//
// One file holds every row of one UTC date (by the timestamp column), all
// columns of the table (features and raw_data_json included), ordered by
// (timestamp, id). The vector index (market_snapshot_vectors) is derived
// from the rows and is not archived.
//
// The files stay on this PC: nothing here sends data anywhere, and the
// directory is neither served by the application's web/MCP endpoints nor
// part of the database backup (the broker's terms forbid third-party
// provision and redistribution of market data).
//
// ArchiveBefore is the contract the purge relies on: it returns only after
// every UTC date before the cutoff is archived AND verified against the
// database, and the result bounds what may be deleted. Any failure returns
// an error and no bound, so the caller deletes nothing (fail closed).
package snapshotarchive

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

const (
	// Table is the archived table's name and the archive's folder name.
	Table = "market_snapshots"

	dirMode  os.FileMode = 0o700
	fileMode os.FileMode = 0o600

	// defaultPageSize bounds how many rows one SELECT reads while a day's file
	// is written, so the single database connection is never held for a whole
	// day of rows.
	defaultPageSize = 5000
)

// DefaultDir is the archive destination: "snapshot_archive" next to the
// SQLite DB at dbPath, a separate folder on the same PC (like
// startup.DefaultBackupDir and DefaultLogDir), derived from dbPath so it
// never depends on the working directory. It does not survive a disk
// failure and is not part of the database backup.
func DefaultDir(dbPath string) string {
	return filepath.Join(filepath.Dir(dbPath), "snapshot_archive")
}

// Archiver archives market_snapshots days of one database into dir.
type Archiver struct {
	db       *sql.DB
	dir      string
	pageSize int
	now      func() time.Time
}

// New returns an Archiver writing under dir (created with mode 0700 when
// missing).
func New(db *sql.DB, dir string) *Archiver {
	return &Archiver{db: db, dir: dir, pageSize: defaultPageSize, now: time.Now}
}

// Dir returns the archive directory.
func (a *Archiver) Dir() string { return a.dir }

// Result is what ArchiveBefore verified.
type Result struct {
	// DeleteBefore is the exclusive upper bound ("YYYY-MM-DD", UTC) of the
	// timestamps whose rows may be deleted: the start of the cutoff's date,
	// so a date is only ever deleted whole.
	DeleteBefore string
	// MaxID is the largest market_snapshots.id that was part of the
	// archived state. Only rows with id <= MaxID may be deleted: a row
	// inserted after the archive was read (a late writer) gets a larger id
	// and is left for the next run, which archives it first.
	MaxID int64
	// Days are the verified dates, oldest first.
	Days []Day
}

// ArchiveBefore archives and verifies every UTC date that has rows
// timestamped before cutoff's UTC date (the cutoff's own date is not
// complete yet, so it is left for a later run). It reads the database at
// the moment of the call: rows inserted afterwards are not covered by
// Result.MaxID.
func (a *Archiver) ArchiveBefore(ctx context.Context, cutoff time.Time) (Result, error) {
	res := Result{DeleteBefore: cutoff.UTC().Format(time.DateOnly)}
	if err := a.db.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(id), 0) FROM market_snapshots`).Scan(&res.MaxID); err != nil {
		return Result{}, fmt.Errorf("snapshotarchive: read max id: %w", err)
	}
	dates, err := a.expiredDates(ctx, res.DeleteBefore, res.MaxID)
	if err != nil {
		return Result{}, err
	}
	for _, date := range dates {
		day, err := a.archiveDay(ctx, date, res.MaxID)
		if err != nil {
			return Result{}, fmt.Errorf("snapshotarchive: archive %s: %w", date, err)
		}
		slog.Info("snapshotarchive: day archived and verified",
			"date", day.Date, "rows", day.Rows, "action", day.Action,
			"path", a.filePath(date), "sha256", day.SHA256)
		res.Days = append(res.Days, day)
	}
	return res, nil
}

// expiredDates lists the distinct UTC dates before deleteBefore that have
// rows with id <= maxID, oldest first (an index range scan over
// market_snapshots_timestamp_idx).
func (a *Archiver) expiredDates(ctx context.Context, deleteBefore string, maxID int64) ([]string, error) {
	rows, err := a.db.QueryContext(ctx,
		`SELECT DISTINCT substr(timestamp, 1, 10) FROM market_snapshots
		 WHERE timestamp < ? AND id <= ? ORDER BY 1`, deleteBefore, maxID)
	if err != nil {
		return nil, fmt.Errorf("snapshotarchive: list expired dates: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var dates []string
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			return nil, fmt.Errorf("snapshotarchive: list expired dates: %w", err)
		}
		dates = append(dates, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("snapshotarchive: list expired dates: %w", err)
	}
	return dates, nil
}

// tableDir is the folder of the archived table.
func (a *Archiver) tableDir() string { return filepath.Join(a.dir, Table) }

// filePath is the final path of the file of date ("YYYY-MM-DD").
func (a *Archiver) filePath(date string) string {
	return filepath.Join(a.tableDir(), date[:4], fmt.Sprintf("%s-%s.csv.gz", Table, date))
}
