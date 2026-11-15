// Package researchtests holds the HTTP-level tests of the research_horizon_summary
// tool (issue #1074). They live apart from mcptests, which holds the other read
// tools' tests.
package researchtests

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/researchlabel"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/researchdiag"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/settings"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

const (
	testAPIKey = "TEST-API-KEY-do-not-leak-0123456789"
	toolName   = "research_horizon_summary"
)

func TestMain(m *testing.M) {
	code := m.Run()
	testkit.RemoveTemplate()
	os.Exit(code)
}

type testServer struct {
	*paperresultsmcp.Server
	http *httptest.Server
}

func newTestServer(t *testing.T, opts paperresultsmcp.Options) *testServer {
	t.Helper()
	s := settings.Default()
	s.Enabled = true
	s.APIKey = testAPIKey
	opts.Settings, opts.CapitalJPY = s, 30_000_000
	opts.Now = func() time.Time { return testkit.FixedNow() }
	opts.Logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
	srv := paperresultsmcp.New(opts)
	hs := httptest.NewServer(srv.Handler())
	t.Cleanup(func() {
		hs.Close()
		_ = srv.Close()
	})
	return &testServer{Server: srv, http: hs}
}

type response struct {
	status int
	body   []byte
}

func (s *testServer) call(t *testing.T, args any) response {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": toolName, "arguments": args}})
	req, err := http.NewRequest(http.MethodPost, s.http.URL, bytes.NewReader(body))
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
	return response{status: res.StatusCode, body: b}
}

// data decodes the tool's structuredContent.data.
func (r response) data(t *testing.T) researchdiag.Data {
	t.Helper()
	if r.status != http.StatusOK {
		t.Fatalf("status %d, body %s", r.status, r.body)
	}
	var env struct {
		Result struct {
			StructuredContent struct {
				Data json.RawMessage `json:"data"`
			} `json:"structuredContent"`
		} `json:"result"`
	}
	if err := json.Unmarshal(r.body, &env); err != nil {
		t.Fatalf("decode %s: %v", r.body, err)
	}
	t.Logf("%s -> %s", toolName, env.Result.StructuredContent.Data)
	var d researchdiag.Data
	if err := json.Unmarshal(env.Result.StructuredContent.Data, &d); err != nil {
		t.Fatalf("decode data %s: %v", env.Result.StructuredContent.Data, err)
	}
	return d
}

// period covers the fixture days (JST 2026-10-13 and 10-14).
var period = map[string]any{"from": "2026-10-10", "to": "2026-10-18"}

func args(extra map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range period {
		out[k] = v
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}

func near(got, want float64) bool { return math.Abs(got-want) < 1e-9 }

func ptr(v float64) *float64 { return &v }

// snapshot times (UTC; the JST day is +9h: 10:00 JST on 10-13 / 10-14).
const (
	s1 = "2026-10-13T01:00:00Z"
	s2 = "2026-10-13T01:01:00Z"
	s3 = "2026-10-13T01:02:00Z"
	s4 = "2026-10-14T01:00:00Z"
	s5 = "2026-10-14T01:01:00Z"
	s6 = "2026-10-15T01:00:00Z" // a day the recorder did not complete
)

// label is one research_horizon_labels row; a nil side leaves its three columns NULL.
type label struct {
	snapshotID  int
	at, horizon string
	spread      *float64
	longGross   *float64
	longCost    float64
	shortGross  *float64
	shortCost   float64
}

func insertLabel(t *testing.T, w *sql.DB, l label) {
	t.Helper()
	var lg, lc, ln, sg, sc, sn any
	if l.longGross != nil {
		lg, lc, ln = *l.longGross, l.longCost, *l.longGross-l.longCost
	}
	if l.shortGross != nil {
		sg, sc, sn = *l.shortGross, l.shortCost, *l.shortGross-l.shortCost
	}
	var spread any
	if l.spread != nil {
		spread = *l.spread
	}
	at, err := time.Parse(time.RFC3339, l.at)
	if err != nil {
		t.Fatal(err)
	}
	testkit.MustExec(t, w, `INSERT INTO research_horizon_labels (snapshot_id, instrument_id, symbol, snapshot_at, day, sample_bucket, horizon, exit_at, entry_price, exit_price,
		spread_bps, long_gross_bps, long_cost_bps, long_net_bps, short_gross_bps, short_cost_bps, short_net_bps, label_version)
		VALUES (?, 1, '7203', ?, ?, ?, ?, ?, 1000, 1010, ?, ?, ?, ?, ?, ?, ?, 1)`,
		l.snapshotID, testkit.Stored(t, l.at), at.In(time.FixedZone("JST", 9*3600)).Format(time.DateOnly), researchlabel.SampleBucket(int64(l.snapshotID)),
		l.horizon, testkit.Stored(t, l.at), spread, lg, lc, ln, sg, sc, sn)
}

func decision(t *testing.T, w *sql.DB, id int, kind, direction, at string) {
	t.Helper()
	testkit.MustExec(t, w, `INSERT INTO jev_decisions (id, instrument_id, symbol, timestamp, decision_type, state_hash, state_json,
		question_version, response_json, direction, confidence, latency_ms, model_id)
		VALUES (?, 1, '7203', ?, ?, 'h', '{}', 'q', '{}', ?, 0.7, 5, 'm')`, id, testkit.Stored(t, at), kind, direction)
}

// seed builds the fixture (all numbers below are computed by hand in the tests):
//
//	30m rows  s1 (day 10-13): LONG gross 30 cost 10, SHORT gross -30 cost 12, spread 8
//	          s2 (10-13):     LONG -10, SHORT 10, spread 14
//	          s3 (10-13):     LONG 5,   SHORT -5, spread 20
//	          s4 (10-14):     no ask: LONG NULL; SHORT 40, spread NULL
//	          s5 (10-14):     LONG 15,  SHORT -15, spread 10
//	60m rows  s1: LONG 50, SHORT -50, spread 8;  s2: LONG 10, SHORT -10, spread 14
//	close     s1: LONG 100, SHORT -100, spread 8
//
// Decisions: trader LONG@s1, SHORT@s2, NONE@s3, LONG@s4 (no LONG result), LONG at a
// time without a row, SHORT before the period; scout LONG@s5, SHORT@s4.
// 10-13 is recorded (4 snapshots, purged from market_snapshots since) and so is 10-14
// (3 snapshots); both through the repository's own MarkDone, which writes the day sums the
// tool reads. 10-15 is not recorded: it has a stray LONG row (gross 999) at s6, a trader
// LONG decision on it and 2 snapshots in market_snapshots, none of which may count.
func seed(t *testing.T) (string, *sql.DB) {
	t.Helper()
	path, w := testkit.NewMigratedDB(t)
	testkit.MustExec(t, w, `INSERT INTO instruments (id, symbol, name, market, is_active, created_at, updated_at) VALUES (1, '7203', 'x', 'TSE Prime', 1, 't', 't')`)
	for _, l := range []label{
		{1, s1, "30m", ptr(8), ptr(30), 10, ptr(-30), 12},
		{2, s2, "30m", ptr(14), ptr(-10), 10, ptr(10), 12},
		{3, s3, "30m", ptr(20), ptr(5), 10, ptr(-5), 12},
		{4, s4, "30m", nil, nil, 10, ptr(40), 12},
		{5, s5, "30m", ptr(10), ptr(15), 10, ptr(-15), 12},
		{1, s1, "60m", ptr(8), ptr(50), 10, ptr(-50), 12},
		{2, s2, "60m", ptr(14), ptr(10), 10, ptr(-10), 12},
		{1, s1, "close", ptr(8), ptr(100), 10, ptr(-100), 12},
	} {
		insertLabel(t, w, l)
	}
	decision(t, w, 1, "trader", "LONG", s1)
	decision(t, w, 2, "trader", "SHORT", s2)
	decision(t, w, 3, "trader", "NONE", s3)
	decision(t, w, 4, "trader", "LONG", s4)
	decision(t, w, 5, "trader", "LONG", "2026-10-14T05:00:00Z")
	decision(t, w, 6, "trader", "SHORT", "2026-09-01T01:00:00Z")
	decision(t, w, 7, "scout", "LONG", s5)
	decision(t, w, 8, "scout", "SHORT", s4)
	insertLabel(t, w, label{6, s6, "30m", ptr(5), ptr(999), 10, nil, 12})
	decision(t, w, 9, "trader", "LONG", s6)
	repo := researchlabel.NewRepository(w)
	if err := repo.MarkDone(context.Background(), "2026-10-13", 4, time.Date(2026, 10, 14, 7, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkDone(context.Background(), "2026-10-14", 3, time.Date(2026, 10, 15, 7, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	for i, ts := range []string{s4, s5, "2026-10-14T01:05:00Z", s6, "2026-10-15T01:05:00Z"} {
		testkit.MustExec(t, w, `INSERT INTO market_snapshots (id, instrument_id, symbol, timestamp, price, volume, turnover, vwap, price_vs_vwap_bps, raw_data_json)
			VALUES (?, 1, '7203', ?, 1000, 1, 1, 1, 1, '{}')`, 4+i, testkit.Stored(t, ts))
	}
	return path, w
}
