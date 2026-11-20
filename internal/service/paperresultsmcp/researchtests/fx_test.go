package researchtests

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/evaldiag"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/researchdiag"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

// The optional fx_symbol argument (issue #1134) adds a descriptive fx_context
// (the same buckets as jev_evaluation's, issue #1084); without it the answer is
// byte-for-byte what the tool returned before the argument existed.

// fxMid records the USD_JPY minute opening at t (RFC3339) with the given mid.
func fxMid(t *testing.T, w *sql.DB, symbol, at string, mid float64) {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, at)
	if err != nil {
		t.Fatal(err)
	}
	testkit.MustExec(t, w, `INSERT INTO research_fx_candles (symbol, open_time, trading_day, bid_open, bid_high, bid_low, bid_close, ask_open, ask_high, ask_low, ask_close, recorded_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 't')`, symbol, sqlutil.FormatTime(ts), ts.Add(-6*time.Hour).Format(time.DateOnly),
		mid-0.005, mid-0.005, mid-0.005, mid-0.005, mid+0.005, mid+0.005, mid+0.005, mid+0.005)
}

// seedFX records USD_JPY minutes for the fixture's snapshots. The match of a time is
// the latest minute at or before it within 5 minutes; the start of the trailing window
// is that match of (time - 60 minutes):
//
//	s1 (10-13 01:00)  now 150.0  start 149.5 (00:00)  up
//	s2 (10-13 01:01)  now 149.0  start 150.0 (00:01)  down
//	s3 (10-13 01:02)  now 150.0  start 150.0 (00:02)  flat
//	s4 (10-14 01:00)  no candle at or before it       unmatched
//	s5 (10-14 01:01)  now 151.0  start 150.0 (00:01)  up
func seedFX(t *testing.T, w *sql.DB) {
	t.Helper()
	for at, mid := range map[string]float64{
		"2026-10-13T00:00:00Z": 149.5, "2026-10-13T00:01:00Z": 150, "2026-10-13T00:02:00Z": 150,
		"2026-10-13T01:00:00Z": 150, "2026-10-13T01:01:00Z": 149, "2026-10-13T01:02:00Z": 150,
		"2026-10-14T00:01:00Z": 150, "2026-10-14T01:01:00Z": 151,
	} {
		fxMid(t, w, "USD_JPY", at, mid)
	}
}

// want is a bucket's row count and mean net (nil while the count is 0).
type want struct {
	n    int
	mean *float64
}

func checkBuckets(t *testing.T, name string, fs *researchdiag.FXSide, rows int, w map[string]want) {
	t.Helper()
	if fs == nil {
		t.Fatalf("%s: no fx side", name)
	}
	if len(fs.Buckets) != 4 {
		t.Fatalf("%s: buckets = %+v, want up, flat, down, unmatched once each", name, fs.Buckets)
	}
	sum := 0
	for i, b := range fs.Buckets {
		if name := []string{evaldiag.FxUp, evaldiag.FxFlat, evaldiag.FxDown, evaldiag.FxUnmatched}[i]; b.Bucket != name {
			t.Errorf("bucket %d is %s, want %s", i, b.Bucket, name)
		}
		sum += b.N
		exp := w[b.Bucket]
		if b.N != exp.n || (exp.mean == nil) != (b.MeanNetBps == nil) || (exp.mean != nil && !near(*b.MeanNetBps, *exp.mean)) {
			t.Errorf("%s: bucket %s = n %d mean %v, want n %d mean %v", name, b.Bucket, b.N, b.MeanNetBps, exp.n, exp.mean)
		}
	}
	if sum != rows {
		t.Errorf("%s: buckets hold %d rows, the side has %d", name, sum, rows)
	}
}

func fxHorizon(t *testing.T, d researchdiag.Data, name string) researchdiag.FXHorizon {
	t.Helper()
	if d.FXContext == nil {
		t.Fatal("no fx_context")
	}
	for _, h := range d.FXContext.Horizons {
		if h.Horizon == name {
			return h
		}
	}
	t.Fatalf("no fx horizon %s", name)
	return researchdiag.FXHorizon{}
}

func TestFXSymbolSplitsTheSnapshotsRowsByTheTrailingMidReturn(t *testing.T) {
	path, w := seed(t)
	seedFX(t, w)
	d := newTestServer(t, paperresultsmcp.Options{DBPath: path}).call(t, args(map[string]any{"fx_symbol": "USD_JPY"})).data(t)

	c := d.FXContext
	if c.Symbol != "USD_JPY" || c.LookbackMinutes != 60 || c.StalenessMinutes != 5 || c.Return == "" || len(c.Horizons) != 3 {
		t.Fatalf("fx_context header = %+v", c)
	}
	h30 := fxHorizon(t, d, "30m")
	if h30.SamplePercent != 100 {
		t.Errorf("30m sample percent = %d, want 100", h30.SamplePercent)
	}
	// LONG: s1 +20 up, s2 -20 down, s3 -5 flat, s5 +5 up (s4 has no long row).
	checkBuckets(t, "30m long", h30.Long, 4, map[string]want{"up": {2, ptr(12.5)}, "down": {1, ptr(-20)}, "flat": {1, ptr(-5)}, "unmatched": {}})
	// SHORT: s1 -42 up, s2 -2 down, s3 -17 flat, s4 +28 unmatched, s5 -27 up.
	checkBuckets(t, "30m short", h30.Short, 5, map[string]want{"up": {2, ptr(-34.5)}, "down": {1, ptr(-2)}, "flat": {1, ptr(-17)}, "unmatched": {1, ptr(28)}})
	h60 := fxHorizon(t, d, "60m")
	checkBuckets(t, "60m long", h60.Long, 2, map[string]want{"up": {1, ptr(40)}, "down": {1, ptr(0)}})
	checkBuckets(t, "60m short", h60.Short, 2, map[string]want{"up": {1, ptr(-62)}, "down": {1, ptr(-22)}})
	hc := fxHorizon(t, d, "close")
	checkBuckets(t, "close long", hc.Long, 1, map[string]want{"up": {1, ptr(90)}})
	checkBuckets(t, "close short", hc.Short, 1, map[string]want{"up": {1, ptr(-112)}})
}

func TestFXSymbolSplitsTheDecisionScopesByTheTrailingMidReturn(t *testing.T) {
	path, w := seed(t)
	seedFX(t, w)
	srv := newTestServer(t, paperresultsmcp.Options{DBPath: path})

	// trader: LONG@s1 (up), SHORT@s2 (down); the LONG@s4 has no long result.
	d := srv.call(t, args(map[string]any{"scope": "trader", "fx_symbol": "USD_JPY"})).data(t)
	h30 := fxHorizon(t, d, "30m")
	checkBuckets(t, "trader 30m long", h30.Long, 1, map[string]want{"up": {1, ptr(20)}})
	checkBuckets(t, "trader 30m short", h30.Short, 1, map[string]want{"down": {1, ptr(-2)}})
	if h60 := fxHorizon(t, d, "60m"); h60.SamplePercent != 100 {
		t.Errorf("a decision scope reads every row: sample percent = %d", h60.SamplePercent)
	}
	checkBuckets(t, "trader close long", fxHorizon(t, d, "close").Long, 1, map[string]want{"up": {1, ptr(90)}})

	// scout: LONG@s5 (up), SHORT@s4 (no candle: unmatched). A side without a row is null.
	d = srv.call(t, args(map[string]any{"scope": "scout", "fx_symbol": "USD_JPY"})).data(t)
	h30 = fxHorizon(t, d, "30m")
	checkBuckets(t, "scout 30m long", h30.Long, 1, map[string]want{"up": {1, ptr(5)}})
	checkBuckets(t, "scout 30m short", h30.Short, 1, map[string]want{"unmatched": {1, ptr(28)}})
	if h60 := fxHorizon(t, d, "60m"); h60.Long != nil || h60.Short != nil {
		t.Errorf("scout 60m has no rows but fx sides are %+v / %+v", h60.Long, h60.Short)
	}

	// direction narrows the decisions as it does the statistics.
	d = srv.call(t, args(map[string]any{"scope": "trader", "direction": "SHORT", "fx_symbol": "USD_JPY"})).data(t)
	if h := fxHorizon(t, d, "30m"); h.Long != nil {
		t.Errorf("direction SHORT still has a long fx side %+v", h.Long)
	}
}

// A pair with no recorded candle splits nothing: every row is unmatched.
func TestFXSymbolWithoutCandlesIsAllUnmatched(t *testing.T) {
	path, w := seed(t)
	seedFX(t, w)
	d := newTestServer(t, paperresultsmcp.Options{DBPath: path}).call(t, args(map[string]any{"fx_symbol": "EUR_USD"})).data(t)
	h30 := fxHorizon(t, d, "30m")
	checkBuckets(t, "30m long", h30.Long, 4, map[string]want{"unmatched": {4, ptr(0)}})
	checkBuckets(t, "30m short", h30.Short, 5, map[string]want{"unmatched": {5, ptr(-12)}})
}

// Without fx_symbol the response is byte-identical to the tool's answer before the
// argument existed (testdata is that answer in each scope, captured from the code of
// main before this change), whether or not an FX record exists; with it, everything
// but the added fx_context equals that answer, and nothing is written.
func TestWithoutFXSymbolTheOutputIsByteIdentical(t *testing.T) {
	path, w := seed(t)
	srv := newTestServer(t, paperresultsmcp.Options{DBPath: path})
	golden := func(scope string) []byte {
		b, err := os.ReadFile("testdata/research_horizon_summary_without_fx_symbol_" + scope + ".json")
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), "fx_context") {
			t.Fatalf("the recorded answer of %s names fx_context", scope)
		}
		return b
	}
	scopes := []string{"snapshots", "trader", "scout"}
	for _, scope := range scopes {
		if got := srv.call(t, args(map[string]any{"scope": scope})).body; string(got) != string(golden(scope)) {
			t.Fatalf("%s: the response without fx_symbol (and without an FX record) differs from the recorded answer: %.300s", scope, got)
		}
	}
	seedFX(t, w)
	before := dumpHashes(t, w)
	for _, scope := range scopes {
		if got := srv.call(t, args(map[string]any{"scope": scope})).body; string(got) != string(golden(scope)) {
			t.Fatalf("%s: an FX record changed the response of a call without fx_symbol", scope)
		}
		with := srv.call(t, args(map[string]any{"scope": scope, "fx_symbol": "USD_JPY"}))
		var g, x struct {
			Result struct {
				StructuredContent struct {
					Data researchdiag.Data `json:"data"`
				} `json:"structuredContent"`
			} `json:"result"`
		}
		if err := json.Unmarshal(golden(scope), &g); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(with.body, &x); err != nil {
			t.Fatal(err)
		}
		if x.Result.StructuredContent.Data.FXContext == nil {
			t.Fatalf("%s: no fx_context", scope)
		}
		x.Result.StructuredContent.Data.FXContext = nil
		if !reflect.DeepEqual(g.Result.StructuredContent.Data, x.Result.StructuredContent.Data) {
			t.Fatalf("%s: fx_symbol changed something besides adding fx_context", scope)
		}
	}
	after := dumpHashes(t, w)
	for table, h := range after {
		if before[table] != h {
			t.Errorf("table %s changed during research_horizon_summary with fx_symbol", table)
		}
	}
}

func TestFXSymbolIsStrictlyValidated(t *testing.T) {
	path, _ := seed(t)
	srv := newTestServer(t, paperresultsmcp.Options{DBPath: path})
	for _, v := range []any{"usd_jpy", "USDJPY", "USD/JPY", "USD_JPY,EUR_USD", 5, strings.Repeat("A", 8)} {
		if r := srv.call(t, args(map[string]any{"fx_symbol": v})); r.status != http.StatusUnprocessableEntity || !strings.Contains(string(r.body), "invalid_argument") {
			t.Errorf("fx_symbol %v: status %d body %s, want 422 invalid_argument", v, r.status, r.body)
		}
	}
	srv.call(t, args(map[string]any{"fx_symbol": "EUR_USD"})).data(t)
}

// dumpHashes hashes every table's rows, so a write anywhere shows.
func dumpHashes(t *testing.T, w *sql.DB) map[string]string {
	t.Helper()
	tables, err := w.QueryContext(t.Context(), `SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for tables.Next() {
		var n string
		if err := tables.Scan(&n); err != nil {
			t.Fatal(err)
		}
		names = append(names, n)
	}
	if err := tables.Err(); err != nil {
		t.Fatal(err)
	}
	_ = tables.Close()
	out := map[string]string{}
	for _, n := range names {
		rows, err := w.QueryContext(t.Context(), `SELECT * FROM "`+n+`"`)
		if err != nil {
			t.Fatal(err)
		}
		cols, _ := rows.Columns()
		h := sha256.New()
		for rows.Next() {
			vals := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				t.Fatal(err)
			}
			_, _ = fmt.Fprintln(h, vals...)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		_ = rows.Close()
		out[n] = fmt.Sprintf("%x", h.Sum(nil))
	}
	return out
}
