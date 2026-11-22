package sessiongap_test

import (
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
	"github.com/ousiassllc/pitha-trador/internal/service/retention"
)

// The record outlives the retention purge of succeeded jobs (7 days): the
// purge deletes the jobs the counts came from and leaves the daily record.
func TestRecordSurvivesTheJobRetentionPurge(t *testing.T) {
	conn := newDB(t)
	now := time.Now().In(jst)
	// The day to record: the previous trading day of now, with a succeeded job finishing on it.
	date := ""
	for d := now.AddDate(0, 0, -1); ; d = d.AddDate(0, 0, -1) {
		if marketcalendar.TSE.IsTradingDay(d) {
			date = d.Format(time.DateOnly)
			break
		}
	}
	// Some history (inside the snapshot retention), then an afternoon-only day.
	snapshots(t, conn, []time.Time{now.AddDate(0, 0, -30)}, minutes(date, "12:30", "15:30"))
	finished := sqlutil.FormatTime(at(date, "13:00"))
	if _, err := conn.Exec(`INSERT INTO jobs (queue, payload_json, status, scheduled_at, finished_at, created_at)
		VALUES ('market-data', '{}', 'succeeded', ?, ?, ?)`, finished, finished, finished); err != nil {
		t.Fatal(err)
	}
	if err := recorder(conn, &clock{now: now}, nil).Check(t.Context()); err != nil {
		t.Fatal(err)
	}
	r, ok := readRow(t, conn, date)
	if !ok || r.kind != "intraday" || !r.jobsOK.Valid || r.jobsOK.Int64 != 1 || r.jobsFailed.Int64 != 0 {
		t.Fatalf("record = %+v (found %v), want intraday with 1 succeeded job counted", r, ok)
	}

	// Age everything past the succeeded-job retention and purge.
	old := sqlutil.FormatTime(now.AddDate(0, 0, -(retention.DefaultSucceededJobDays + 3)))
	if _, err := conn.Exec(`UPDATE jobs SET finished_at = ?`, old); err != nil {
		t.Fatal(err)
	}
	if err := retention.New(conn, retention.Policy{}).Purge(t.Context()); err != nil {
		t.Fatalf("Purge: %v", err)
	}
	var jobs int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM jobs`).Scan(&jobs); err != nil || jobs != 0 {
		t.Fatalf("jobs after purge = %d (%v), want 0: the purge must have deleted them", jobs, err)
	}
	after, ok := readRow(t, conn, date)
	if !ok || after.kind != "intraday" || !after.jobsOK.Valid || after.jobsOK.Int64 != 1 {
		t.Errorf("record after the purge = %+v (found %v), want it unchanged", after, ok)
	}
}

// A day older than the job retention is recorded without job counts: the
// purged jobs would read as zero otherwise.
func TestJobCountsAreOmittedForADayOlderThanTheJobRetention(t *testing.T) {
	conn := newDB(t)
	snapshots(t, conn, fullDay("2026-10-01")[0], fullDay("2026-10-01")[1])
	// Left in progress on 10-02 and recorded at a start 20 days later.
	if _, err := conn.Exec(`INSERT INTO session_diagnostics_daily (session_date, gap_kind, recorded_at) VALUES ('2026-10-02', 'in_progress', ?)`,
		sqlutil.FormatTime(at("2026-10-02", "10:00"))); err != nil {
		t.Fatal(err)
	}
	c := &clock{now: at("2026-10-22", "09:00")}
	if err := recorder(conn, c, nil).Check(t.Context()); err != nil {
		t.Fatal(err)
	}
	r, ok := readRow(t, conn, "2026-10-02")
	if !ok || r.kind == "in_progress" || r.jobsOK.Valid || r.jobsFailed.Valid {
		t.Errorf("10-02 = %+v (found %v), want recorded with null job counts", r, ok)
	}
}
