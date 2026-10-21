package jobfailures

import (
	"database/sql"
	"fmt"
	"testing"

	_ "modernc.org/sqlite"
)

// errorCodeSQL reduces each message to its code token inside SQL: 700
// messages that differ only in the symbol name are two groups, and the
// realistic formats give the codes the former Go regexp
// (`(?:\bcode |p_errno=|sResultCode=)(-?[0-9]{1,8})\b`) gave.
func TestErrorCodeSQLReducesMessagesToCodes(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`CREATE TABLE jobs (queue TEXT, status TEXT, finished_at TEXT, last_error TEXT)`); err != nil {
		t.Fatal(err)
	}
	const finished = "2026-10-20T01:00:00Z"
	insert := func(msg any) {
		if _, err := db.Exec(`INSERT INTO jobs VALUES ('market-data', 'failed', ?, ?)`, finished, msg); err != nil {
			t.Fatal(err)
		}
	}
	for i := range 600 {
		insert(fmt.Sprintf(`marketdatajob: fetch board for "S%d": marketdata: kabu station api error (http 429, code 4001006): name %d`, i, i))
	}
	for i := range 100 {
		insert(fmt.Sprintf(`marketdatajob: fetch board for "S%d": marketdata: kabu station api error (http 401, code 4001007): name %d`, i, i))
	}
	single := map[string]string{
		"tachibana: p_errno=-62: text":                      "-62",
		"tachibana: sResultCode=11001 text":                 "11001",
		"context deadline exceeded":                         "",
		"code 12345678901 too long":                         "12345678901", // SQL keeps 10 digits; the Go side rejects it
		"kabu station api error (http 400, code 4002006)":   "4002006",
		"kabu station api error (http 400, code 4002006)  ": "4002006",
	}
	for msg := range single {
		insert(msg)
	}
	insert(nil)

	rows, err := db.Query(`SELECT day, code, COUNT(*) FROM (`+errorCodeSQL("queue = ? AND status = 'failed' AND finished_at >= ? AND finished_at < ?")+`) GROUP BY 1, 2`,
		"market-data", "2026-10-19T00:00:00Z", "2026-10-21T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	got := map[string]int{}
	for rows.Next() {
		var day, code string
		var n int
		if err := rows.Scan(&day, &code, &n); err != nil {
			t.Fatal(err)
		}
		if day != "2026-10-20" {
			t.Errorf("day = %q, want the JST day 2026-10-20", day)
		}
		got[code] += n
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	want := map[string]int{
		"4001006": 600, "4001007": 100, "4002006": 2, "-62": 1, "11001": 1,
		"":           2, // no code token (the message without one, and NULL)
		"1234567890": 1, // 10 digits cut from "code 12345678901": not a token, so Go-side check maps it to unknown
	}
	if len(got) != len(want) {
		t.Fatalf("groups = %v, want %v: one per code, not per message", got, want)
	}
	for code, n := range want {
		if got[code] != n {
			t.Errorf("code %q = %d, want %d (all: %v)", code, got[code], n, got)
		}
	}
	if !jobErrorCodeToken.MatchString("4001006") || jobErrorCodeToken.MatchString("1234567890") {
		t.Error("jobErrorCodeToken must accept 7 digits and reject 10")
	}
}
