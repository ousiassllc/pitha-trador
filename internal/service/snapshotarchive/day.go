package snapshotarchive

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// dbDay is what the database holds for one UTC date (rows with id <= the
// run's MaxID).
type dbDay struct {
	IDs            []int64 // ordered by (timestamp, id)
	FirstTimestamp string
	LastTimestamp  string
}

// readDBDay reads the ids and timestamp range of date's rows.
func (a *Archiver) readDBDay(ctx context.Context, date string, maxID int64) (dbDay, error) {
	next, err := nextDate(date)
	if err != nil {
		return dbDay{}, err
	}
	rows, err := a.db.QueryContext(ctx,
		`SELECT id, timestamp FROM market_snapshots
		 WHERE timestamp >= ? AND timestamp < ? AND id <= ? ORDER BY timestamp, id`, date, next, maxID)
	if err != nil {
		return dbDay{}, err
	}
	defer func() { _ = rows.Close() }()
	var d dbDay
	for rows.Next() {
		var id int64
		var ts string
		if err := rows.Scan(&id, &ts); err != nil {
			return dbDay{}, err
		}
		if len(d.IDs) == 0 {
			d.FirstTimestamp = ts
		}
		d.LastTimestamp = ts
		d.IDs = append(d.IDs, id)
	}
	return d, rows.Err()
}

func nextDate(date string) (string, error) {
	t, err := time.Parse(time.DateOnly, date)
	if err != nil {
		return "", fmt.Errorf("bad date %q: %w", date, err)
	}
	return t.AddDate(0, 0, 1).Format(time.DateOnly), nil
}

// archiveDay makes sure date's rows are in a verified archive file and
// records it in the manifest. Cases, by the existing file (if any):
//
//   - absent: write it from the database;
//   - covers every database row: keep it untouched (idempotent re-run; also
//     the state after a purge was interrupted part-way through the date,
//     when the file legitimately holds more rows than the database);
//   - lacks database rows (a late writer inserted into an archived date):
//     rewrite it as the union of the file's rows and the database's, so
//     rows already purged from the database stay in the archive and the file
//     never shrinks;
//   - unreadable/corrupt, or from a different column layout: an error.
//     Nothing is overwritten, so no archive ever loses rows, and the purge
//     stays blocked until someone looks.
func (a *Archiver) archiveDay(ctx context.Context, date string, maxID int64) (Day, error) {
	db, err := a.readDBDay(ctx, date, maxID)
	if err != nil {
		return Day{}, err
	}
	if len(db.IDs) == 0 {
		return Day{}, errors.New("no rows in the database any more")
	}
	if err := os.MkdirAll(filepath.Dir(a.filePath(date)), dirMode); err != nil {
		return Day{}, fmt.Errorf("create archive folder: %w", err)
	}
	existing, err := scanFile(a.filePath(date), date)
	action := "written"
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return Day{}, fmt.Errorf("existing archive is unusable (left untouched): %w", err)
	default:
		if missing, _ := compareIDs(db.IDs, existing.IDs); missing == 0 {
			return a.record(date, existing, "verified")
		}
		action = "rewritten"
	}
	info, err := a.writeDayFile(ctx, date, maxID, db, existing)
	if err != nil {
		return Day{}, err
	}
	return a.record(date, info, action)
}

// compareIDs counts the database ids absent from the archive and the
// archive ids absent from the database.
func compareIDs(dbIDs []int64, archived map[int64]struct{}) (missing, extra int) {
	inDB := make(map[int64]struct{}, len(dbIDs))
	for _, id := range dbIDs {
		inDB[id] = struct{}{}
		if _, ok := archived[id]; !ok {
			missing++
		}
	}
	for id := range archived {
		if _, ok := inDB[id]; !ok {
			extra++
		}
	}
	return missing, extra
}

// record stores the verified day in the manifest and returns it.
func (a *Archiver) record(date string, info fileInfo, action string) (Day, error) {
	m, err := ReadManifest(a.dir)
	if err != nil {
		return Day{}, err
	}
	day := Day{
		Date: date, Rows: info.Rows,
		FirstTimestamp: info.FirstTimestamp, LastTimestamp: info.LastTimestamp,
		SHA256: info.SHA256,
		File:   filepath.ToSlash(filepath.Join(date[:4], filepath.Base(a.filePath(date)))),
		Action: action, ArchivedAt: a.now().UTC().Format(time.RFC3339),
	}
	if prev, ok := m.find(date); ok && prev.SHA256 == day.SHA256 {
		day.ArchivedAt = prev.ArchivedAt
	}
	if m.upsert(day) {
		if err := a.writeManifest(m); err != nil {
			return Day{}, fmt.Errorf("write manifest: %w", err)
		}
	}
	return day, nil
}
