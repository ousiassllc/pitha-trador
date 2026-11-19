// Package researchbarstests holds the HTTP-level tests of the
// research_daily_bars_status tool (issue #1082).
package researchbarstests

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/researchbarsdiag"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/settings"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

const testAPIKey = "TEST-API-KEY-do-not-leak-0123456789"

func TestMain(m *testing.M) {
	code := m.Run()
	testkit.RemoveTemplate()
	os.Exit(code)
}

type reply struct {
	status int
	body   []byte
}

func call(t *testing.T, path string, args any) reply {
	t.Helper()
	s := settings.Default()
	s.Enabled, s.APIKey = true, testAPIKey
	srv := paperresultsmcp.New(paperresultsmcp.Options{
		DBPath: path, Settings: s, CapitalJPY: 30_000_000,
		Now: func() time.Time { return testkit.FixedNow() }, Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)),
	})
	hs := httptest.NewServer(srv.Handler())
	t.Cleanup(func() { hs.Close(); _ = srv.Close() })
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": "research_daily_bars_status", "arguments": args}})
	req, err := http.NewRequest(http.MethodPost, hs.URL, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+testAPIKey)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return reply{res.StatusCode, b}
}

func (r reply) data(t *testing.T) researchbarsdiag.Data {
	t.Helper()
	var env struct {
		Result struct {
			IsError           bool `json:"isError"`
			StructuredContent struct {
				Data json.RawMessage `json:"data"`
			} `json:"structuredContent"`
		} `json:"result"`
	}
	if err := json.Unmarshal(r.body, &env); err != nil || r.status != http.StatusOK || env.Result.IsError {
		t.Fatalf("status %d, body %s, err %v", r.status, r.body, err)
	}
	var d researchbarsdiag.Data
	if err := json.Unmarshal(env.Result.StructuredContent.Data, &d); err != nil {
		t.Fatalf("decode %s: %v", env.Result.StructuredContent.Data, err)
	}
	return d
}

func bar(t *testing.T, w *sql.DB, symbol, date string) {
	t.Helper()
	testkit.MustExec(t, w, `INSERT INTO daily_bars (symbol, trade_date, open, high, low, close, volume, adj_open, adj_high, adj_low, adj_close, adj_volume)
		VALUES (?, ?, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1)`, symbol, date)
}

func progress(t *testing.T, w *sql.DB, symbol, night string, ok int, firstOK any, saved, failures int) {
	t.Helper()
	testkit.MustExec(t, w, `INSERT INTO research_daily_bar_progress (symbol, last_night, last_ok, first_ok_night, bars_saved, failures, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, 't')`, symbol, night, ok, firstOK, saved, failures)
}

func run(t *testing.T, w *sql.DB, date, status string, symbols, saved, failed int, reason string) {
	t.Helper()
	testkit.MustExec(t, w, `INSERT INTO research_daily_bar_runs (run_date, status, started_at, finished_at, symbols, requests, saved_bars, failed, duration_ms, cursor, error)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1500, '', ?)`, date, status, testkit.Stored(t, date+"T10:00:00Z"), testkit.Stored(t, date+"T10:00:02Z"), symbols, symbols, saved, failed, reason)
}

// Nothing recorded yet: an "unknown" state, zero counts, empty lists, no percent.
func TestEmptyDatabaseReportsUnknownAndZeroes(t *testing.T) {
	path, _ := testkit.NewMigratedDB(t)
	d := call(t, path, map[string]any{}).data(t)
	if d.Status.State != "unknown" || d.Status.UpdatedAt != nil || d.DailyBars.Symbols != 0 || d.DailyBars.FirstTradeDate != nil ||
		d.Backfill.Percent != nil || len(d.Runs) != 0 || len(d.BarsPerDay) != 0 {
		t.Fatalf("data = %+v", d)
	}
}

// The counts come from daily_bars, the progress table and the run rows.
func TestReadsCountsBackfillRunsAndBarsPerDay(t *testing.T) {
	path, w := testkit.NewMigratedDB(t)
	// 1001: 2019-01-04 .. 2026-10-14, 1002 only from 2026-10-13, 1003 only 2026-10-14.
	bar(t, w, "1001", "2019-01-04")
	for _, day := range []string{"2026-10-12", "2026-10-13", "2026-10-14"} {
		bar(t, w, "1001", day)
	}
	bar(t, w, "1002", "2026-10-13")
	bar(t, w, "1002", "2026-10-14")
	bar(t, w, "1003", "2026-10-14")
	testkit.MustExec(t, w, `INSERT INTO research_tachibana_status (component, state, detail, updated_at) VALUES ('bars', 'error', ?, ?)`,
		"2026-10-14 の夜は途中で止まりました（再開します）／セッションがありません・停止しました: SENTINEL_BROKER_TEXT", testkit.Stored(t, "2026-10-14T10:00:00Z"))
	progress(t, w, "1001", "2026-10-14", 1, "2026-10-13", 4, 0)
	progress(t, w, "1002", "2026-10-14", 1, "2026-10-13", 2, 1)
	progress(t, w, "1003", "2026-10-14", 0, nil, 0, 3) // never answered
	progress(t, w, "9999", "2026-10-13", 1, "2026-10-13", 7, 0)
	run(t, w, "2026-10-13", "succeeded", 4, 13, 0, "")
	run(t, w, "2026-10-14", "failed", 3, 6, 1, "セッションがありません・停止しました: SENTINEL_BROKER_TEXT")

	r := call(t, path, map[string]any{})
	d := r.data(t)
	if strings.Contains(string(r.body), "SENTINEL_BROKER_TEXT") {
		t.Fatalf("the broker's wording reached the output: %s", r.body)
	}
	if d.Status.State != "error" || d.Status.UpdatedAt == nil || *d.Status.UpdatedAt != "2026-10-14T10:00:00Z" ||
		d.Status.Detail != "2026-10-14 の夜は途中で止まりました（再開します）／セッションがありません・停止しました" {
		t.Errorf("status = %+v", d.Status)
	}
	if d.DailyBars.Symbols != 3 || *d.DailyBars.FirstTradeDate != "2019-01-04" || *d.DailyBars.LastTradeDate != "2026-10-14" || d.DailyBars.ResearchBarsSaved != 13 {
		t.Errorf("daily_bars = %+v", d.DailyBars)
	}
	// The universe is the latest run's 3; 3 symbols have a first success (9999 too): capped by 100%.
	if b := d.Backfill; b.Universe != 3 || b.Backfilled != 3 || b.FailedSymbols != 1 || b.Percent == nil || *b.Percent != 100 {
		t.Errorf("backfill = %+v", b)
	}
	if len(d.Runs) != 2 || d.Runs[0].RunDate != "2026-10-14" || d.Runs[0].Status != "failed" || d.Runs[0].Failed != 1 || d.Runs[0].DurationMS != 1500 ||
		d.Runs[0].Reason != "セッションがありません・停止しました" || d.Runs[1].Reason != "" || d.Runs[1].SavedBars != 13 {
		t.Errorf("runs = %+v", d.Runs)
	}
	want := []researchbarsdiag.DayBars{{TradeDate: "2026-10-14", Bars: 3}, {TradeDate: "2026-10-13", Bars: 2}, {TradeDate: "2026-10-12", Bars: 1}}
	if !reflect.DeepEqual(d.BarsPerDay, want) {
		t.Errorf("bars_per_day = %v, want %v (only the last 30 days, newest first)", d.BarsPerDay, want)
	}
}

// Only the ten latest nights and trade dates are listed; the percent rounds to one decimal.
func TestListsAreCappedAtTen(t *testing.T) {
	path, w := testkit.NewMigratedDB(t)
	for day := 1; day <= 12; day++ {
		date := time.Date(2026, 10, day, 0, 0, 0, 0, time.UTC).Format(time.DateOnly)
		bar(t, w, "1001", date)
		run(t, w, date, "succeeded", 3, 1, 0, "")
	}
	progress(t, w, "1001", "2026-10-12", 1, "2026-10-01", 12, 0)
	d := call(t, path, map[string]any{}).data(t)
	if len(d.Runs) != 10 || d.Runs[0].RunDate != "2026-10-12" || d.Runs[9].RunDate != "2026-10-03" {
		t.Errorf("runs = %+v", d.Runs)
	}
	if len(d.BarsPerDay) != 10 || d.BarsPerDay[9].TradeDate != "2026-10-03" {
		t.Errorf("bars_per_day = %+v", d.BarsPerDay)
	}
	if d.Backfill.Percent == nil || *d.Backfill.Percent != 33.3 {
		t.Errorf("percent = %v, want 33.3 (1 of 3)", d.Backfill.Percent)
	}
}

// The tool takes no arguments: any is refused.
func TestRejectsArguments(t *testing.T) {
	path, _ := testkit.NewMigratedDB(t)
	for _, args := range []map[string]any{{"from": "2026-10-01"}, {"limit": 5}} {
		r := call(t, path, args)
		if !strings.Contains(string(r.body), "invalid_argument") {
			t.Errorf("args %v: body %s, want invalid_argument", args, r.body)
		}
	}
}
