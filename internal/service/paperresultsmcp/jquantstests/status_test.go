package jquantstests

import (
	"bytes"
	"database/sql"
	"net/http"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

// The fixed "now" of the server is 2026-10-18 12:00 JST, so today is 2026-10-18.
func seed(t *testing.T) (string, *sql.DB) {
	t.Helper()
	path, w := testkit.NewMigratedDB(t)
	for _, stmt := range []string{
		`INSERT INTO research_jquants_daily_bars (day, code, symbol, open, fetched_at) VALUES ('2026-10-15', '72030', '7203', 1, 't'), ('2026-10-16', '72030', '7203', 1, 't'), ('2026-09-01', '72030', '7203', 1, 't')`,
		`INSERT INTO research_jquants_minute_bars (code, symbol, day, bar_at, open, high, low, close, volume, turnover, fetched_at)
			VALUES ('72030', '7203', '2026-10-16', '2026-10-16T00:00:00.000000000Z', 1, 1, 1, 1, 1, 1, 't')`,
		`INSERT INTO research_jquants_symbols (as_of, code, symbol, fetched_at) VALUES ('2026-10-16', '72030', '7203', 't')`,
		`INSERT INTO research_jquants_fetch_log (day, dataset, requests, rows_stored, failures, last_failure_code, last_success_at)
			VALUES ('2026-10-18', 'daily', 5, 2, 1, 'http_429', '2026-10-18T02:30:00.000000000Z'), ('2026-10-17', 'minute', 3, 1, 0, '', '2026-10-17T02:30:00.000000000Z'),
			       ('2026-09-01', 'daily', 9, 9, 9, 'http_500', NULL)`,
	} {
		testkit.MustExec(t, w, stmt)
	}
	return path, w
}

func TestStatusReturnsCountsLastSuccessRequestsAndFailures(t *testing.T) {
	path, w := seed(t)
	for k, v := range map[string]string{"jquants.enabled": "1", "jquants.plan_daily": "1", "jquants.daily_request_cap": "40"} {
		testkit.Setting(t, w, k, v)
	}
	d := newTestServer(t, paperresultsmcp.Options{DBPath: path}).call(t, period).data(t)
	if !d.Enabled || !d.PlanDaily || d.PlanMinute || d.DailyRequestCap != 40 || d.RequestsToday != 5 {
		t.Errorf("state = %+v, want on, daily plan only, cap 40, 5 requests today", d)
	}
	if d.Rows["daily_bars"] != 3 || d.Rows["minute_bars"] != 1 || d.Rows["symbols"] != 1 || d.Rows["coverage"] != 0 {
		t.Errorf("rows = %v", d.Rows)
	}
	if d.DailyDaysInPeriod != 2 || d.MinuteDaysInPeriod != 1 {
		t.Errorf("days in the period = %d daily / %d minute, want 2 / 1", d.DailyDaysInPeriod, d.MinuteDaysInPeriod)
	}
	if d.FailuresRecent != 1 || d.LastFailureCode != "http_429" || d.LastSuccessAt == nil || *d.LastSuccessAt != "2026-10-18T02:30:00Z" {
		t.Errorf("failures %d code %q last success %v", d.FailuresRecent, d.LastFailureCode, d.LastSuccessAt)
	}
}

func TestStatusDefaultsAreOffAndEmpty(t *testing.T) {
	path, _ := testkit.NewMigratedDB(t)
	d := newTestServer(t, paperresultsmcp.Options{DBPath: path}).call(t, period).data(t)
	if d.Enabled || d.PlanDaily || d.PlanMinute || d.DailyRequestCap != 200 || d.RequestsToday != 0 || d.LastSuccessAt != nil || d.LastFailureCode != "" {
		t.Errorf("default status = %+v, want off, nothing subscribed, cap 200 and empty", d)
	}
}

// The response carries counts, dates and fixed codes only: no price, no code, no key.
func TestStatusReturnsNoSecretsOrMarketData(t *testing.T) {
	path, _ := seed(t)
	res := newTestServer(t, paperresultsmcp.Options{DBPath: path}).call(t, period)
	for _, forbidden := range []string{"7203", "72030", "JQUANTS_API_KEY", "x-api-key", "test-key-xxxx", "secrets"} {
		if bytes.Contains(res.body, []byte(forbidden)) {
			t.Errorf("the response contains %q", forbidden)
		}
	}
}

// Unknown arguments are refused, and the MCP connection can neither turn the adapter
// on, change its cap, request the deletion nor delete a row.
func TestStatusArgumentsAreStrictAndTheConnectionIsReadOnly(t *testing.T) {
	path, w := seed(t)
	s := newTestServer(t, paperresultsmcp.Options{DBPath: path})
	for name, a := range map[string]map[string]any{
		"an unknown argument":  {"from": "2026-10-10", "to": "2026-10-18", "enable": 1},
		"a switch as argument": {"from": "2026-10-10", "to": "2026-10-18", "jquants.enabled": 1},
		"a purge as argument":  {"from": "2026-10-10", "to": "2026-10-18", "purge": true},
	} {
		if r := s.call(t, a); r.status != http.StatusUnprocessableEntity || !strings.Contains(string(r.body), "invalid_argument") {
			t.Errorf("%s: status %d body %s, want 422 invalid_argument", name, r.status, r.body)
		}
	}
	db, err := s.DB()
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`DELETE FROM research_jquants_daily_bars`,
		`UPDATE research_jquants_fetch_log SET requests = 0`,
		`INSERT INTO runtime_settings (key, value, updated_at) VALUES ('jquants.enabled', '1', 't')`,
		`INSERT INTO runtime_settings (key, value, updated_at) VALUES ('jquants.purge_requested', '1', 't')`,
	} {
		if _, err := db.Exec(q); err == nil {
			t.Errorf("write succeeded on the MCP connection: %s", testkit.FirstLine(q))
		}
	}
	var n int
	if err := w.QueryRow(`SELECT COUNT(*) FROM runtime_settings WHERE key LIKE 'jquants.%'`).Scan(&n); err != nil || n != 0 {
		t.Errorf("the tool left %d jquants settings, err %v", n, err)
	}
	if err := w.QueryRow(`SELECT COUNT(*) FROM research_jquants_daily_bars`).Scan(&n); err != nil || n != 3 {
		t.Errorf("daily bars = %d (%v), want the 3 seeded", n, err)
	}
}
