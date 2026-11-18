package jquantstests

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"

	jqservice "github.com/ousiassllc/pitha-trador/internal/service/marketdata/jquants"
)

// The sample responses (the saved fixtures) land in the research tables as the
// API documents them: both pages of the daily bars, the minute bars with their JST
// time stored as UTC, the master, and a no-trade day as null prices. The key goes
// out in the x-api-key header only and appears in no stored value.
func TestBackfill_StoresDailyAndMinuteBarsFromTheMockResponses(t *testing.T) {
	r := newRig(t, testKey, settingsFor(true, true, 100))
	r.seedSnapshot(t, "2026-11-16T05:00:00.000000000Z", 3000, 2999, 3001)

	res, err := r.backfiller(0).Run(context.Background(), jqservice.Request{})
	if err != nil || res.Skipped != "" || res.More || res.CapReached {
		t.Fatalf("Run = (%+v, %v)", res, err)
	}
	if res.DailyRows != 3 || res.MinuteRows != 3 || len(res.DailyDays) != 1 || len(res.MinuteDays) != 1 {
		t.Errorf("result = %+v, want 3 daily rows, 3 minute rows on one day each", res)
	}

	if got := r.count(t, "research_jquants_symbols"); got != 2 {
		t.Errorf("symbols = %d, want 2", got)
	}
	var symbol, name, market string
	if err := r.db.QueryRow(`SELECT symbol, company_name, market_name FROM research_jquants_symbols WHERE code = '72030'`).Scan(&symbol, &name, &market); err != nil ||
		symbol != "7203" || name != "トヨタ自動車" || market != "プライム" {
		t.Errorf("master row = (%q, %q, %q, %v)", symbol, name, market, err)
	}

	if got := r.count(t, "research_jquants_daily_bars"); got != 3 {
		t.Fatalf("daily bars = %d, want 3 (both pages)", got)
	}
	var open, high, low, closePrice, volume, adjClose, adjFactor float64
	var day, source string
	err = r.db.QueryRow(`SELECT day, source, open, high, low, close, volume, adj_close, adj_factor FROM research_jquants_daily_bars WHERE code = '67580'`).
		Scan(&day, &source, &open, &high, &low, &closePrice, &volume, &adjClose, &adjFactor)
	if err != nil || day != backfillDay || source != "jquants" || open != 4100 || high != 4120 || low != 4050 || closePrice != 4060 || volume != 5000000 || adjClose != 2030 || adjFactor != 0.5 {
		t.Errorf("daily bar = (%s %s %v %v %v %v %v %v %v) err %v", day, source, open, high, low, closePrice, volume, adjClose, adjFactor, err)
	}
	var nullOpen sql.NullFloat64
	if err := r.db.QueryRow(`SELECT open FROM research_jquants_daily_bars WHERE code = '13010'`).Scan(&nullOpen); err != nil || nullOpen.Valid {
		t.Errorf("a no-trade day's open = %v (err %v), want NULL", nullOpen, err)
	}

	// 3 usable minute rows: 09:00 JST = 00:00Z, 09:01, and the 15:30 closing record; the null row is dropped.
	rows, err := r.db.Query(`SELECT bar_at, day, symbol, open, close, volume, turnover FROM research_jquants_minute_bars WHERE code = '72030' ORDER BY bar_at`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var got []string
	for rows.Next() {
		var at, d, s string
		var o, c, v, va float64
		if err := rows.Scan(&at, &d, &s, &o, &c, &v, &va); err != nil {
			t.Fatal(err)
		}
		got = append(got, strings.Join([]string{at, d, s}, " ")+" "+fmt.Sprint(o, c, v, va))
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"2026-11-16T00:00:00.000000000Z 2026-11-16 7203 3000 3004 125000 3.75e+08",
		"2026-11-16T00:01:00.000000000Z 2026-11-16 7203 3004 3008 80000 2.4e+08",
		"2026-11-16T06:30:00.000000000Z 2026-11-16 7203 3040 3040 900000 2.736e+09",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("minute bars =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	// Every request carried the key in the header, none in the URL; the key is in no stored value.
	for _, req := range r.server.all() {
		if req.Key != testKey {
			t.Errorf("request %s lacked the x-api-key header", req.Path)
		}
		if strings.Contains(req.Query, testKey) {
			t.Errorf("the key is in the query of %s", req.Path)
		}
	}
	for _, table := range []string{"research_jquants_symbols", "research_jquants_daily_bars", "research_jquants_minute_bars", "research_jquants_coverage", "research_jquants_fetch_log"} {
		if dump := dumpTable(t, r.db, table); strings.Contains(dump, testKey) {
			t.Errorf("%s contains the API key", table)
		}
	}
}

// A second run fetches nothing: every unit has its coverage mark, and the rows
// stay as they are. Writing the same rows again is an upsert on the natural keys.
func TestBackfill_IsIdempotentAndFetchesOnlyMissingDays(t *testing.T) {
	r := newRig(t, testKey, settingsFor(true, true, 100))
	r.seedSnapshot(t, "2026-11-16T05:00:00.000000000Z", 3000, 2999, 3001)
	b := r.backfiller(0)
	if _, err := b.Run(context.Background(), jqservice.Request{}); err != nil {
		t.Fatal(err)
	}
	first := r.server.count() // master + 2 daily pages + 1 minute
	if first != 4 {
		t.Fatalf("first run sent %d requests, want 4", first)
	}
	res, err := b.Run(context.Background(), jqservice.Request{})
	if err != nil || res.Calls != 0 || res.More {
		t.Fatalf("second Run = (%+v, %v), want no calls", res, err)
	}
	if n := r.server.count(); n != first {
		t.Fatalf("second run sent %d more requests, want 0", n-first)
	}

	// The upsert itself: the same bars written again keep one row per key and take the new values.
	bars, err := r.client.DailyBars(context.Background(), backfillDay)
	if err != nil {
		t.Fatal(err)
	}
	*bars[0].Close = 3100
	for range 2 {
		if _, err := r.repo.UpsertDailyBars(context.Background(), bars, r.now); err != nil {
			t.Fatal(err)
		}
	}
	var c float64
	if err := r.db.QueryRow(`SELECT close FROM research_jquants_daily_bars WHERE code = '72030'`).Scan(&c); err != nil || c != 3100 || r.count(t, "research_jquants_daily_bars") != 3 {
		t.Errorf("after the re-write: close %v (err %v), rows %d; want 3100 and 3 rows", c, err, r.count(t, "research_jquants_daily_bars"))
	}
}

// The weekend and exchange holidays are not asked for; a window over a weekend
// requests the two trading days only.
func TestBackfill_SkipsNonTradingDays(t *testing.T) {
	r := newRig(t, testKey, settingsFor(true, false, 100))
	if _, err := r.backfiller(0).Run(context.Background(), jqservice.Request{From: "2026-11-13", To: "2026-11-16"}); err != nil {
		t.Fatal(err)
	}
	var days []string
	for _, req := range r.server.all() {
		if req.Path == "/v2/equities/bars/daily" && !strings.Contains(req.Query, "pagination_key") {
			days = append(days, strings.TrimPrefix(req.Query, "date="))
		}
	}
	if strings.Join(days, ",") != "2026-11-13,2026-11-16" {
		t.Errorf("daily requests for %v, want Friday and Monday only", days)
	}
}

// Today is never fetched (its bars are incomplete): a window ending today stops at yesterday.
func TestBackfill_NeverFetchesToday(t *testing.T) {
	r := newRig(t, testKey, settingsFor(true, false, 100))
	if _, err := r.backfiller(0).Run(context.Background(), jqservice.Request{From: "2026-11-17", To: "2026-11-30"}); err != nil {
		t.Fatal(err)
	}
	if n := r.server.countPath("bars/daily"); n != 0 {
		t.Errorf("%d daily requests for today or later, want 0", n)
	}
}

// The minute bars are delivered the day after: an empty answer for a recent day
// is "not delivered yet" and is retried, an empty answer for an old day is "no trades".
func TestBackfill_EmptyMinuteResponseIsRetriedOnlyForRecentDays(t *testing.T) {
	r := newRig(t, testKey, settingsFor(false, true, 100))
	r.server.minuteEmpty = true
	r.seedSnapshot(t, "2026-11-16T05:00:00.000000000Z", 3000, 2999, 3001)
	r.seedSnapshot(t, "2026-11-04T05:00:00.000000000Z", 3000, 2999, 3001)
	if _, err := r.backfiller(0).Run(context.Background(), jqservice.Request{From: "2026-11-04", To: "2026-11-16"}); err != nil {
		t.Fatal(err)
	}
	var marked []string
	rows, err := r.db.Query(`SELECT day FROM research_jquants_coverage WHERE dataset = 'minute' ORDER BY day`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			t.Fatal(err)
		}
		marked = append(marked, d)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if strings.Join(marked, ",") != "2026-11-04" {
		t.Errorf("minute coverage = %v, want only the old day marked (the recent one is retried)", marked)
	}
}

func dumpTable(t *testing.T, db *sql.DB, table string) string {
	t.Helper()
	rows, err := db.Query(`SELECT * FROM ` + table)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	cols, _ := rows.Columns()
	var sb strings.Builder
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatal(err)
		}
		for _, v := range vals {
			sb.WriteString(fmt.Sprint(v) + "|")
		}
		sb.WriteString("\n")
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return sb.String()
}
