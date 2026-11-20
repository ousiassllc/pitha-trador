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

// The optional index_symbol argument (issue #1084) adds a descriptive
// index_context to every cohort; without it the answer is byte-for-byte what the
// tool returned before the argument existed.

// indexValue writes the recorded value of index 101 for the minute starting at t.
func indexValue(t *testing.T, w *sql.DB, at time.Time, price float64) {
	t.Helper()
	testkit.MustExec(t, w, `INSERT INTO research_index_minutes (symbol, minute_time, trading_day, price, via, recorded_at) VALUES ('101', ?, ?, ?, 'event', 't')`,
		sqlutil.FormatTime(at), at.In(time.FixedZone("JST", 9*3600)).Format(time.DateOnly), price)
}

// seedIndex records minutes (UTC) for the decisions of small(). A value is known 5
// seconds after its minute starts; the match of a time is the latest known minute
// within 5 minutes, and the start of the trailing 30-minute window is that match
// of (decision time - 30 minutes):
//
//	id  decision     now                            start of window         bucket     llm net
//	1   10-05 00:00  101 (10-04 23:59; the 00:00     100 (10-04 23:29)       up         +30
//	                 minute, 999, is not known yet)
//	2   10-05 00:20  100 (00:19)                     101 (10-04 23:49)       down       -30
//	3   10-06 00:00  100 (10-05 23:59)               100 (10-05 23:29)       flat       -30
//	4   10-06 00:20  none (23:59 is 21 minutes old)  -                       unmatched  +30
//	5, 6, 7 ...      none                            -                       unmatched
func seedIndex(t *testing.T, w *sql.DB) {
	t.Helper()
	put := func(day, hm string, p float64) { indexValue(t, w, utc(day, hm), p) }
	put("2026-10-04", "23:29", 100)
	put("2026-10-04", "23:49", 101)
	put("2026-10-04", "23:59", 101)
	put("2026-10-05", "00:00", 999)
	put("2026-10-05", "00:19", 100)
	put("2026-10-05", "23:29", 100)
	put("2026-10-05", "23:59", 100)
}

func indexBuckets(t *testing.T, c evaldiag.Cohort) map[string]evaldiag.FxBucket {
	t.Helper()
	if c.IndexContext == nil {
		t.Fatal("no index_context")
	}
	out := map[string]evaldiag.FxBucket{}
	for _, b := range c.IndexContext.Buckets {
		out[b.Bucket] = b
	}
	if len(out) != 4 || len(c.IndexContext.Buckets) != 4 {
		t.Fatalf("buckets = %+v, want up, flat, down, unmatched once each", c.IndexContext.Buckets)
	}
	return out
}

func TestIndexSymbolSplitsTheLLMArmByTheTrailingIndexReturn(t *testing.T) {
	srv, w := newServer(t)
	seedSmall(t, w)
	seedIndex(t, w)
	d := srv.call(t, map[string]any{"index_symbol": "101"}).data(t)

	cohort := cohortOf(t, d, m1, v3)
	a := indexBuckets(t, cohort)
	wantBucket(t, a, evaldiag.FxUp, 1, ptr(30))
	wantBucket(t, a, evaldiag.FxDown, 1, ptr(-30))
	wantBucket(t, a, evaldiag.FxFlat, 1, ptr(-30))
	n := 0
	for _, b := range a {
		n += b.N
	}
	if n != cohort.Evaluated || a[evaldiag.FxUnmatched].N != cohort.Evaluated-3 {
		t.Errorf("cohort A buckets hold %d rows (unmatched %d), the cohort evaluated %d", n, a[evaldiag.FxUnmatched].N, cohort.Evaluated)
	}
	b := indexBuckets(t, cohortOf(t, d, m2, v4))
	if c := cohortOf(t, d, m2, v4); b[evaldiag.FxUnmatched].N != c.Evaluated {
		t.Errorf("cohort B unmatched = %d, want all %d rows", b[evaldiag.FxUnmatched].N, c.Evaluated)
	}
	c := cohort.IndexContext
	if c.Symbol != "101" || c.LookbackMinutes != 30 || c.StalenessMinutes != 5 || c.Return == "" {
		t.Errorf("index_context header = %+v", c)
	}
	if cohort.FXContext != nil {
		t.Error("index_symbol alone added an fx_context")
	}
	// Both contexts can be asked for at once.
	both := cohortOf(t, srv.call(t, map[string]any{"index_symbol": "101", "fx_symbol": "USD_JPY"}).data(t), m1, v3)
	if both.IndexContext == nil || both.FXContext == nil {
		t.Error("both index_context and fx_context are expected")
	}
}

// A code with no recorded value splits nothing: every row is unmatched.
func TestIndexSymbolWithoutValuesIsAllUnmatched(t *testing.T) {
	srv, w := newServer(t)
	seedSmall(t, w)
	seedIndex(t, w)
	a := cohortOf(t, srv.call(t, map[string]any{"index_symbol": "151"}).data(t), m1, v3)
	wantBucket(t, indexBuckets(t, a), evaldiag.FxUnmatched, a.Evaluated, ptr(-5))
}

// Without index_symbol the response is byte-identical to the recorded answer from
// before the argument existed, whether or not an index record exists; with it,
// everything but the added index_context equals that answer, and nothing is written.
func TestWithoutIndexSymbolTheOutputIsByteIdentical(t *testing.T) {
	golden, err := os.ReadFile("testdata/jev_evaluation_without_fx_symbol.json")
	if err != nil {
		t.Fatal(err)
	}
	srv, w := newServer(t)
	seedSmall(t, w)
	seedIndex(t, w)
	if got := srv.call(t, nil).body; string(got) != string(golden) {
		t.Fatalf("an index record changed the response of a call without index_symbol: %.300s", got)
	}
	if strings.Contains(string(golden), "index_context") {
		t.Fatal("the recorded answer names index_context")
	}
	before := dump(t, w)
	with := srv.call(t, map[string]any{"index_symbol": "101"})
	for table, h := range dump(t, w) {
		if before[table] != h {
			t.Errorf("table %s changed during jev_evaluation with index_symbol", table)
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
		if x.Result.StructuredContent.Data.Cohorts[i].IndexContext == nil {
			t.Fatal("a cohort has no index_context")
		}
		x.Result.StructuredContent.Data.Cohorts[i].IndexContext = nil
	}
	if !reflect.DeepEqual(g.Result.StructuredContent.Data, x.Result.StructuredContent.Data) {
		t.Fatal("index_symbol changed something besides adding index_context")
	}
}

func TestIndexSymbolIsStrictlyValidated(t *testing.T) {
	srv, _ := newServer(t)
	for _, v := range []any{"1 01", "101,151", "10-1", 5, strings.Repeat("1", 9)} {
		srv.call(t, map[string]any{"index_symbol": v}).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidArgument)
	}
	srv.call(t, map[string]any{"index_symbol": "151"}).data(t)
}
