package evaltests

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/evaldiag"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

// The optional fx_symbol argument (issue #1084) adds a descriptive fx_context to
// every cohort; without it the answer is byte-for-byte what the tool returned
// before the argument existed.

// fxMid writes the recorded USD/JPY minute at t with the given mid (BID mid-0.005,
// ASK mid+0.005); a side of 0 leaves that side NULL.
func fxMid(t *testing.T, w *sql.DB, symbol string, at time.Time, bid, ask float64) {
	t.Helper()
	side := func(v float64) []any {
		if v == 0 {
			return []any{nil, nil, nil, nil}
		}
		return []any{v, v, v, v}
	}
	args := append([]any{symbol, sqlutil.FormatTime(at), at.Add(-6 * time.Hour).In(time.FixedZone("JST", 9*3600)).Format(time.DateOnly)}, side(bid)...)
	args = append(args, side(ask)...)
	testkit.MustExec(t, w, `INSERT INTO research_fx_candles (symbol, open_time, trading_day, bid_open, bid_high, bid_low, bid_close, ask_open, ask_high, ask_low, ask_close, recorded_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 't')`, args...)
}

func utc(day, hm string) time.Time {
	ts, err := time.Parse("2006-01-02 15:04", day+" "+hm)
	if err != nil {
		panic(err)
	}
	return ts
}

func mid(m float64) (float64, float64) { return m - 0.005, m + 0.005 }

// seedFX records USD_JPY minutes (UTC) for the decisions of small(). The match of
// a time is the latest minute at or before it within 5 minutes; the start of the
// trailing window is that match of (decision time - 60 minutes):
//
//	id  decision        now                start of window    bucket     llm net
//	1   10-05 00:00     150.00 (10-05 00:00)  149.50 (10-04 23:00)  up         +30
//	2   10-05 00:20     150.00 (10-05 00:20)  150.50 (10-04 23:20)  down       -30
//	3   10-06 00:00     150.00 (10-06 00:00)  150.00 (10-05 23:00)  flat       -30
//	4   10-06 00:20     none (00:00 is 20 min old)  150.00 (10-05 23:20)  unmatched  +30
//	5   10-07 00:00     151.00 (10-06 23:57, 3 min old)  150.00 (10-06 22:59)  up   0
//	6   10-07 00:20     none (00:14 is 6 min old)  150.00 (10-06 23:20)  unmatched  -30
//	7   10-07 01:00     152.00 (00:58; the BID-only minute at 01:00 is skipped)  151.00 (10-06 23:57)  up  +30
//	8, 14               none (00:58 on 10-07 is a day old)  -  unmatched
func seedFX(t *testing.T, w *sql.DB) {
	t.Helper()
	put := func(day, hm string, m float64) {
		b, a := mid(m)
		fxMid(t, w, "USD_JPY", utc(day, hm), b, a)
	}
	put("2026-10-04", "23:00", 149.50)
	put("2026-10-04", "23:20", 150.50)
	put("2026-10-05", "00:00", 150.00)
	put("2026-10-05", "00:20", 150.00)
	put("2026-10-05", "23:00", 150.00)
	put("2026-10-05", "23:20", 150.00)
	put("2026-10-06", "00:00", 150.00)
	put("2026-10-06", "22:59", 150.00)
	put("2026-10-06", "23:20", 150.00)
	put("2026-10-06", "23:57", 151.00)
	put("2026-10-07", "00:14", 151.00)
	put("2026-10-07", "00:58", 152.00)
	fxMid(t, w, "USD_JPY", utc("2026-10-07", "01:00"), 140, 0) // BID only at the decision: skipped
}

func fxBuckets(t *testing.T, c evaldiag.Cohort) map[string]evaldiag.FxBucket {
	t.Helper()
	if c.FXContext == nil {
		t.Fatal("no fx_context")
	}
	out := map[string]evaldiag.FxBucket{}
	for _, b := range c.FXContext.Buckets {
		out[b.Bucket] = b
	}
	if len(out) != 4 || len(c.FXContext.Buckets) != 4 {
		t.Fatalf("buckets = %+v, want up, flat, down, unmatched once each", c.FXContext.Buckets)
	}
	return out
}

func wantBucket(t *testing.T, got map[string]evaldiag.FxBucket, name string, n int, mean *float64) {
	t.Helper()
	b := got[name]
	if b.N != n || (mean == nil) != (b.MeanNetBps == nil) || (mean != nil && *b.MeanNetBps-*mean > eps) || (mean != nil && *mean-*b.MeanNetBps > eps) {
		t.Errorf("bucket %s = n %d mean %v, want n %d mean %v", name, b.N, deref(b.MeanNetBps), n, deref(mean))
	}
}

func ptr(v float64) *float64 { return &v }

func TestFXSymbolSplitsTheLLMArmByTheTrailingMidReturn(t *testing.T) {
	srv, w := newServer(t)
	seedSmall(t, w)
	seedFX(t, w)
	d := srv.call(t, map[string]any{"fx_symbol": "USD_JPY"}).data(t)

	a := fxBuckets(t, cohortOf(t, d, m1, v3))
	wantBucket(t, a, evaldiag.FxUp, 2, ptr(15)) // ids 1 (+30) and 5 (0)
	wantBucket(t, a, evaldiag.FxDown, 1, ptr(-30))
	wantBucket(t, a, evaldiag.FxFlat, 1, ptr(-30))
	wantBucket(t, a, evaldiag.FxUnmatched, 2, ptr(0)) // ids 4 (+30) and 6 (-30)
	n := 0
	for _, b := range a {
		n += b.N
	}
	if c := cohortOf(t, d, m1, v3); n != c.Evaluated {
		t.Errorf("cohort A buckets hold %d rows, the cohort evaluated %d", n, c.Evaluated)
	}

	b := fxBuckets(t, cohortOf(t, d, m2, v4))
	wantBucket(t, b, evaldiag.FxUp, 1, ptr(30)) // id 7: the BID-only minute at the decision is skipped for the one 2 minutes earlier
	wantBucket(t, b, evaldiag.FxUnmatched, 2, ptr(5))
	wantBucket(t, b, evaldiag.FxDown, 0, nil)
	wantBucket(t, b, evaldiag.FxFlat, 0, nil)

	ctx := cohortOf(t, d, m1, v3).FXContext
	if ctx.Symbol != "USD_JPY" || ctx.LookbackMinutes != 60 || ctx.StalenessMinutes != 5 || ctx.Return == "" {
		t.Errorf("fx_context header = %+v", ctx)
	}
}

// A pair with no recorded candle splits nothing: every row is unmatched.
func TestFXSymbolWithoutCandlesIsAllUnmatched(t *testing.T) {
	srv, w := newServer(t)
	seedSmall(t, w)
	seedFX(t, w)
	d := srv.call(t, map[string]any{"fx_symbol": "EUR_USD"}).data(t)
	a := cohortOf(t, d, m1, v3)
	wantBucket(t, fxBuckets(t, a), evaldiag.FxUnmatched, a.Evaluated, ptr(-5))
}

// Without fx_symbol the response is byte-identical to the tool's answer before the
// argument existed (testdata is that answer, captured from the code of main before
// this change), whether or not an FX record exists; with it, everything but the
// added fx_context equals that answer, and nothing is written.
func TestWithoutFXSymbolTheOutputIsByteIdentical(t *testing.T) {
	golden, err := os.ReadFile("testdata/jev_evaluation_without_fx_symbol.json")
	if err != nil {
		t.Fatal(err)
	}
	srv, w := newServer(t)
	seedSmall(t, w)
	if got := srv.call(t, nil).body; string(got) != string(golden) {
		t.Fatalf("the response without fx_symbol (and without an FX record) differs from the recorded answer: %.300s", got)
	}
	seedFX(t, w)
	if got := srv.call(t, nil).body; string(got) != string(golden) {
		t.Fatal("an FX record changed the response of a call without fx_symbol")
	}
	if strings.Contains(string(golden), "fx_context") {
		t.Fatal("the recorded answer names fx_context")
	}

	before := dump(t, w)
	with := srv.call(t, map[string]any{"fx_symbol": "USD_JPY"})
	for table, h := range dump(t, w) {
		if before[table] != h {
			t.Errorf("table %s changed during jev_evaluation with fx_symbol", table)
		}
	}
	var g, x struct {
		Result struct {
			StructuredContent struct {
				Data evaldiag.Data `json:"data"`
			} `json:"structuredContent"`
		} `json:"result"`
	}
	if err := json.Unmarshal(golden, &g); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(with.body, &x); err != nil {
		t.Fatal(err)
	}
	for i := range x.Result.StructuredContent.Data.Cohorts {
		if x.Result.StructuredContent.Data.Cohorts[i].FXContext == nil {
			t.Fatal("a cohort has no fx_context")
		}
		x.Result.StructuredContent.Data.Cohorts[i].FXContext = nil
	}
	if !reflect.DeepEqual(g.Result.StructuredContent.Data, x.Result.StructuredContent.Data) {
		t.Fatal("fx_symbol changed something besides adding fx_context")
	}
}

func TestFXSymbolIsStrictlyValidated(t *testing.T) {
	srv, _ := newServer(t)
	for _, v := range []any{"usd_jpy", "USDJPY", "USD/JPY", "USD_JPY,EUR_USD", 5, strings.Repeat("A", 8)} {
		srv.call(t, map[string]any{"fx_symbol": v}).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidArgument)
	}
	srv.call(t, map[string]any{"fx_symbol": "EUR_USD"}).data(t)
}
