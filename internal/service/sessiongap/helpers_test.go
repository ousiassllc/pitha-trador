package sessiongap_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
	"github.com/ousiassllc/pitha-trador/internal/service/sessiongap"
)

func newDB(t *testing.T) *sql.DB {
	t.Helper()
	conn, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha.db"))
	if err != nil {
		t.Fatalf("sqlitedb.Open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if _, err := conn.Exec(`INSERT INTO instruments (id, symbol, name, market) VALUES (1, '7203', 'n', 'TSE')`); err != nil {
		t.Fatalf("instrument: %v", err)
	}
	return conn
}

// snapshots stores one snapshot per given minute.
func snapshots(t *testing.T, conn *sql.DB, at ...[]time.Time) {
	t.Helper()
	for _, list := range at {
		for _, ts := range list {
			_, err := conn.Exec(`INSERT INTO market_snapshots (instrument_id, symbol, timestamp, price, bid, ask, volume,
				turnover, vwap, price_vs_vwap_bps, raw_data_json) VALUES (1, '7203', ?, 1, 1, 1, 1, 1, 1, 1, '{}')`, sqlutil.FormatTime(ts))
			if err != nil {
				t.Fatalf("snapshot: %v", err)
			}
		}
	}
}

func fullDay(date string) [][]time.Time {
	return [][]time.Time{minutes(date, "09:00", "11:30"), minutes(date, "12:30", "15:30")}
}

type row struct {
	kind               string
	codes, alerts      []string
	checks, rows, mins int
	first, last        sql.NullString
	jobsOK, jobsFailed sql.NullInt64
	longest            int
}

func readRow(t *testing.T, conn *sql.DB, date string) (row, bool) {
	t.Helper()
	var r row
	var codes, alerts string
	err := conn.QueryRow(`SELECT gap_kind, gap_codes_json, alert_codes_json, watch_checks, snapshot_rows, snapshot_minutes,
		first_snapshot_at, last_snapshot_at, longest_gap_minutes, market_data_jobs_succeeded, market_data_jobs_failed
		FROM session_diagnostics_daily WHERE session_date = ?`, date).
		Scan(&r.kind, &codes, &alerts, &r.checks, &r.rows, &r.mins, &r.first, &r.last, &r.longest, &r.jobsOK, &r.jobsFailed)
	if errors.Is(err, sql.ErrNoRows) {
		return row{}, false
	}
	if err != nil {
		t.Fatalf("read %s: %v", date, err)
	}
	if err := json.Unmarshal([]byte(codes), &r.codes); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(alerts), &r.alerts); err != nil {
		t.Fatal(err)
	}
	return r, true
}

type clock struct{ now time.Time }

func (c *clock) Now() time.Time { return c.now }

func recorder(conn *sql.DB, c *clock, alerts func() []string) *sessiongap.Recorder {
	return sessiongap.NewRecorder(sessiongap.Config{
		Store: sessiongap.NewDBStore(conn),
		Alerts: func(context.Context) ([]string, error) {
			if alerts == nil {
				return nil, nil
			}
			return alerts(), nil
		},
		Now: c.Now,
	})
}

func at(date, hm string) time.Time {
	ts, err := time.ParseInLocation("2006-01-02 15:04", date+" "+hm, jst)
	if err != nil {
		panic(err)
	}
	return ts
}
