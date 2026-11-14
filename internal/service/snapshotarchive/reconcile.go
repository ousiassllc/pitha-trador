package snapshotarchive

import (
	"context"
	"fmt"
	"math"
	"os"
)

// Reconcile statuses.
const (
	// StatusMatch: the archive holds exactly the database's rows of the date
	// (count, ids, first and last timestamp). The state to expect between
	// archiving and the purge.
	StatusMatch = "match"
	// StatusArchiveOnly: every database row of the date is in the archive,
	// which holds more (or the database holds none): the date was purged.
	StatusArchiveOnly = "archive-only"
	// StatusMismatch: the archive lacks database rows of the date, or the
	// file is missing/unusable.
	StatusMismatch = "mismatch"
)

// Reconciliation compares the archive file of one UTC date with the
// database.
type Reconciliation struct {
	Date               string
	Status             string
	ArchiveRows        int
	DBRows             int
	ArchiveFirst       string
	ArchiveLast        string
	DBFirst            string
	DBLast             string
	MissingFromArchive int // database rows (by id) that the archive lacks
	SHA256             string
	ManifestMatch      bool // the manifest entry equals the file (rows, range, sha256)
	Problem            string
}

// String renders the reconciliation as the one-block text the issue's
// "row count check" evidence is quoted from.
func (r Reconciliation) String() string {
	s := fmt.Sprintf(
		"%s %s: status=%s\n  archive: rows=%d first=%s last=%s sha256=%s\n  database: rows=%d first=%s last=%s\n  missing_from_archive=%d manifest_match=%t",
		Table, r.Date, r.Status, r.ArchiveRows, r.ArchiveFirst, r.ArchiveLast, r.SHA256,
		r.DBRows, r.DBFirst, r.DBLast, r.MissingFromArchive, r.ManifestMatch)
	if r.Problem != "" {
		s += "\n  problem: " + r.Problem
	}
	return s
}

// Reconcile re-reads the archive file of date ("YYYY-MM-DD", UTC) and
// compares it with the database's current rows of that date and with the
// manifest. It writes nothing. A missing or unreadable file is reported as
// StatusMismatch with Problem set, not as an error; an error means the
// comparison itself could not run.
func (a *Archiver) Reconcile(ctx context.Context, date string) (Reconciliation, error) {
	r := Reconciliation{Date: date, Status: StatusMismatch}
	dbd, err := a.readDBDay(ctx, date, math.MaxInt64)
	if err != nil {
		return r, fmt.Errorf("snapshotarchive: read database day %s: %w", date, err)
	}
	r.DBRows, r.DBFirst, r.DBLast = len(dbd.IDs), dbd.FirstTimestamp, dbd.LastTimestamp

	info, err := scanFile(a.filePath(date), date)
	if err != nil {
		if os.IsNotExist(err) {
			r.Problem = "archive file does not exist"
		} else {
			r.Problem = err.Error()
		}
		r.MissingFromArchive = r.DBRows
		return r, nil
	}
	r.ArchiveRows, r.ArchiveFirst, r.ArchiveLast, r.SHA256 = info.Rows, info.FirstTimestamp, info.LastTimestamp, info.SHA256
	r.MissingFromArchive, _ = compareIDs(dbd.IDs, info.IDs)

	m, err := ReadManifest(a.dir)
	if err != nil {
		return r, err
	}
	if e, ok := m.find(date); ok {
		r.ManifestMatch = e.Rows == info.Rows && e.SHA256 == info.SHA256 &&
			e.FirstTimestamp == info.FirstTimestamp && e.LastTimestamp == info.LastTimestamp
	}
	switch {
	case r.MissingFromArchive > 0:
	case r.ArchiveRows == r.DBRows && r.ArchiveFirst == r.DBFirst && r.ArchiveLast == r.DBLast:
		r.Status = StatusMatch
	default:
		r.Status = StatusArchiveOnly
	}
	return r, nil
}
