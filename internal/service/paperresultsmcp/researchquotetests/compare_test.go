// Package researchquotetests holds the HTTP-level tests of the
// research_quote_source_compare tool (issue #1083).
package researchquotetests

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/researchquotediag"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/settings"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

const (
	testAPIKey = "TEST-API-KEY-do-not-leak-0123456789"
	toolName   = "research_quote_source_compare"
)

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
		"params": map[string]any{"name": toolName, "arguments": args}})
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
	return reply{status: res.StatusCode, body: b}
}

func (r reply) data(t *testing.T) researchquotediag.Data {
	t.Helper()
	if r.status != http.StatusOK {
		t.Fatalf("status %d, body %s", r.status, r.body)
	}
	var env struct {
		Result struct {
			StructuredContent struct {
				Data researchquotediag.Data `json:"data"`
			} `json:"structuredContent"`
		} `json:"result"`
	}
	if err := json.Unmarshal(r.body, &env); err != nil {
		t.Fatalf("decode %s: %v", r.body, err)
	}
	t.Logf("%s", r.body)
	return env.Result.StructuredContent.Data
}

// period covers the fixture days (JST 2026-10-12 .. 10-15).
var period = map[string]any{"from": "2026-10-12", "to": "2026-10-16"}

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

func near(got *float64, want float64) bool { return got != nil && math.Abs(*got-want) < 1e-9 }

func exec(t *testing.T, w *sql.DB, query string, args ...any) { testkit.MustExec(t, w, query, args...) }

// sample inserts a 立花 sample; at is the minute (UTC).
func sample(t *testing.T, w *sql.DB, at, symbol string, price float64, phase, via string) {
	t.Helper()
	exec(t, w, `INSERT INTO research_quote_samples (timestamp, source, symbol, price, via, session_phase) VALUES (?, 'tachibana', ?, ?, ?, ?)`,
		testkit.Stored(t, at), symbol, price, via, phase)
}

// snap inserts a kabu snapshot of instrument id.
func snap(t *testing.T, w *sql.DB, id int, at, symbol string, price float64) {
	t.Helper()
	exec(t, w, `INSERT INTO market_snapshots (instrument_id, symbol, timestamp, price, volume, turnover, vwap, price_vs_vwap_bps, raw_data_json)
		VALUES (?, ?, ?, ?, 1, 1, 1, 1, '{}')`, id, symbol, testkit.Stored(t, at), price)
}

// seed builds the fixture; every number the tests expect is computed by hand
// from it. Times are UTC (09:00 JST = 00:00Z, so the JST day is the UTC date).
//
// 2026-10-13 立花: 7203 00:00 1000 (open_auction, event), 00:10 1001 (morning), 00:20 1010 (morning, rest),
//
//	02:00 1000 (morning; kabu has none), 06:20 1100 (close); 6758 00:10 2000 (morning)
//
// 2026-10-13 kabu:  7203 00:00 at :05 price 900 then at :50 price 1000 (the last one counts),
//
//	00:10 1000, 00:20 1000, 03:00 1000 (lunch; 立花 has none), 06:20 1000; 6758 00:10 2000, 00:30 2000 (立花 has none)
//
// 2026-10-14 立花: 7203 00:00 1000; kabu 7203 00:00 1000 and 00:01 1000 (立花 has none)
// 2026-10-12 and 10-15: kabu rows only (no 立花 sample, so the days are not compared).
func seed(t *testing.T) string {
	t.Helper()
	path, w := testkit.NewMigratedDB(t)
	exec(t, w, `INSERT INTO instruments (id, symbol, name, market, is_active, created_at, updated_at) VALUES (1, '7203', 'x', 'TSE Prime', 1, 't', 't'), (2, '6758', 'y', 'TSE Prime', 1, 't', 't')`)
	for _, s := range []struct {
		at, symbol string
		price      float64
		phase, via string
	}{
		{"2026-10-13T00:00:00Z", "7203", 1000, "open_auction", "event"},
		{"2026-10-13T00:10:00Z", "7203", 1001, "morning", "event"},
		{"2026-10-13T00:20:00Z", "7203", 1010, "morning", "rest"},
		{"2026-10-13T02:00:00Z", "7203", 1000, "morning", "event"},
		{"2026-10-13T06:20:00Z", "7203", 1100, "close", "event"},
		{"2026-10-13T00:10:00Z", "6758", 2000, "morning", "event"},
		{"2026-10-14T00:00:00Z", "7203", 1000, "open_auction", "event"},
	} {
		sample(t, w, s.at, s.symbol, s.price, s.phase, s.via)
	}
	for _, s := range []struct {
		id         int
		at, symbol string
		price      float64
	}{
		{1, "2026-10-13T00:00:05Z", "7203", 900}, {1, "2026-10-13T00:00:50Z", "7203", 1000},
		{1, "2026-10-13T00:10:30Z", "7203", 1000}, {1, "2026-10-13T00:20:30Z", "7203", 1000},
		{1, "2026-10-13T03:00:30Z", "7203", 1000}, {1, "2026-10-13T06:20:30Z", "7203", 1000},
		{2, "2026-10-13T00:10:30Z", "6758", 2000}, {2, "2026-10-13T00:30:30Z", "6758", 2000},
		{1, "2026-10-14T00:00:30Z", "7203", 1000}, {1, "2026-10-14T00:01:30Z", "7203", 1000},
		{1, "2026-10-12T00:00:30Z", "7203", 1}, {1, "2026-10-15T00:00:30Z", "7203", 1},
	} {
		snap(t, w, s.id, s.at, s.symbol, s.price)
	}
	exec(t, w, `INSERT INTO research_tachibana_status (component, state, detail, updated_at) VALUES ('quotes', 'running', '記録中: 銘柄 2', ?)`, testkit.Stored(t, "2026-10-14T00:30:00Z"))
	return path
}

func TestCompareAgainstTheHandComputedFixture(t *testing.T) {
	d := call(t, seed(t), args(nil)).data(t)

	if d.Status.State != "running" || d.Status.Detail != "記録中: 銘柄 2" || d.Status.UpdatedAt == nil {
		t.Errorf("status = %+v", d.Status)
	}
	if len(d.Days) != 2 || d.Days[0].Date != "2026-10-13" || d.Days[1].Date != "2026-10-14" {
		t.Fatalf("days = %+v, want only the two days with 立花 samples", d.Days)
	}
	d1, d2 := d.Days[0], d.Days[1]
	if got, want := d1.Tachibana, (researchquotediag.SourceStats{Rows: 6, Symbols: 2, Minutes: 5, ViaEvent: 5, ViaREST: 1}); got != want {
		t.Errorf("10-13 立花 = %+v, want %+v", got, want)
	}
	if got, want := d1.Kabu, (researchquotediag.SourceStats{Rows: 8, Symbols: 2, Minutes: 6}); got != want {
		t.Errorf("10-13 kabu = %+v, want %+v (the two snapshots of 00:00 are two rows, one minute)", got, want)
	}
	if got, want := d2.Tachibana, (researchquotediag.SourceStats{Rows: 1, Symbols: 1, Minutes: 1, ViaEvent: 1}); got != want {
		t.Errorf("10-14 立花 = %+v, want %+v", got, want)
	}
	if c := d1.Comparison; c.MinutesBoth != 5 || c.OnlyKabu != 2 || c.OnlyTachibana != 1 {
		t.Errorf("10-13 comparison = %+v, want both 5, only kabu 2 (lunch 7203, 6758 00:30), only 立花 1 (7203 02:00)", c)
	}
	if c := d2.Comparison; c.MinutesBoth != 1 || c.OnlyKabu != 1 || c.OnlyTachibana != 0 {
		t.Errorf("10-14 comparison = %+v", c)
	}

	if want := (researchquotediag.SourceStats{Rows: 7, Symbols: 2, Minutes: 6, ViaEvent: 6, ViaREST: 1}); d.Tachibana != want {
		t.Errorf("total 立花 = %+v, want %+v", d.Tachibana, want)
	}
	if want := (researchquotediag.SourceStats{Rows: 10, Symbols: 2, Minutes: 8}); d.Kabu != want {
		t.Errorf("total kabu = %+v, want %+v", d.Kabu, want)
	}
	// Gaps (立花 - kabu) / kabu in bps over the six pairs: 0, +10, +100, +1000, 0 (6758), 0 (10-14).
	c := d.Comparison
	if c.MinutesBoth != 6 || c.OnlyKabu != 3 || c.OnlyTachibana != 1 || c.Gap.Pairs != 6 {
		t.Fatalf("total comparison = %+v", c)
	}
	g := c.Gap
	for name, pair := range map[string]struct {
		got  *float64
		want float64
	}{
		"mean_abs": {g.MeanAbsBps, 185}, "median_abs": {g.MedianBps, 5}, "p95_abs": {g.P95AbsBps, 1000}, "max_abs": {g.MaxAbsBps, 1000},
		"mean_signed": {g.MeanBps, 185}, "above5": {g.ShareAbove5, 0.5}, "above10": {g.ShareAbove10, 2.0 / 6}, "above50": {g.ShareAbove50, 2.0 / 6},
	} {
		if !near(pair.got, pair.want) {
			t.Errorf("gap %s = %v, want %v", name, pair.got, pair.want)
		}
	}

	phases := map[string]researchquotediag.PhaseComparison{}
	var order []string
	for _, p := range d.ByPhase {
		phases[p.Phase] = p
		order = append(order, p.Phase)
	}
	if strings.Join(order, ",") != "open_auction,morning,lunch,close" {
		t.Errorf("phases = %v, want the phases that have minutes, in session order", order)
	}
	if p := phases["open_auction"]; p.MinutesBoth != 2 || p.OnlyKabu != 1 || p.OnlyTachibana != 0 || !near(p.Gap.MeanAbsBps, 0) {
		t.Errorf("open_auction = %+v, want 2 pairs of gap 0 and kabu's 10-14 00:01 only", p)
	}
	m := phases["morning"]
	if m.MinutesBoth != 3 || m.OnlyKabu != 1 || m.OnlyTachibana != 1 || !near(m.Gap.MeanAbsBps, 110.0/3) || !near(m.Gap.MedianBps, 10) ||
		!near(m.Gap.P95AbsBps, 100) || !near(m.Gap.MaxAbsBps, 100) || !near(m.Gap.ShareAbove5, 2.0/3) || !near(m.Gap.ShareAbove10, 1.0/3) || !near(m.Gap.ShareAbove50, 1.0/3) {
		t.Errorf("morning = %+v, want gaps 0, 10, 100", m)
	}
	if p := phases["lunch"]; p.MinutesBoth != 0 || p.OnlyKabu != 1 || p.Gap.MeanAbsBps != nil {
		t.Errorf("lunch = %+v, want only kabu's 12:00 snapshot (phase from its time) and null gaps", p)
	}
	if p := phases["close"]; p.MinutesBoth != 1 || !near(p.Gap.MaxAbsBps, 1000) {
		t.Errorf("close = %+v", p)
	}

	if !d.Scan.Complete || d.Scan.CoveredThrough == nil || *d.Scan.CoveredThrough != "2026-10-15" || d.Scan.RowsRead != 7+10 || d.Notice == "" {
		t.Errorf("scan = %+v", d.Scan)
	}
}

func TestSymbolFilterAndEmptyPeriods(t *testing.T) {
	path := seed(t)
	d := call(t, path, args(map[string]any{"symbol": "6758"})).data(t)
	if d.Symbol == nil || *d.Symbol != "6758" || len(d.Days) != 1 {
		t.Fatalf("symbol/days = %v/%+v, want 6758 and only 10-13", d.Symbol, d.Days)
	}
	if c := d.Comparison; c.MinutesBoth != 1 || c.OnlyKabu != 1 || c.OnlyTachibana != 0 || !near(c.Gap.MaxAbsBps, 0) {
		t.Errorf("6758 comparison = %+v", c)
	}
	if d.Tachibana.Rows != 1 || d.Kabu.Rows != 2 {
		t.Errorf("6758 rows 立花 %d kabu %d, want 1 and 2", d.Tachibana.Rows, d.Kabu.Rows)
	}

	// A period without 立花 samples: no days, null gaps, still the status.
	empty := call(t, path, map[string]any{"from": "2026-10-15", "to": "2026-10-17"}).data(t)
	if len(empty.Days) != 0 || empty.Comparison.Gap.Pairs != 0 || empty.Comparison.Gap.MeanAbsBps != nil || empty.Status.State != "running" || !empty.Scan.Complete {
		t.Errorf("empty period = %+v", empty)
	}
	// No research rows at all: status unknown.
	path2, _ := testkit.NewMigratedDB(t)
	none := call(t, path2, args(nil)).data(t)
	if none.Status.State != "unknown" || len(none.Days) != 0 {
		t.Errorf("empty database = %+v", none)
	}
}

func TestBadArgumentsAreRejected(t *testing.T) {
	path := seed(t)
	for name, a := range map[string]map[string]any{
		"unknown argument": args(map[string]any{"limit": 5}),
		"bad symbol":       args(map[string]any{"symbol": "7203; DROP TABLE x"}),
		"symbol type":      args(map[string]any{"symbol": 7203}),
	} {
		if r := call(t, path, a); r.status == http.StatusOK {
			t.Errorf("%s: accepted (%s)", name, r.body)
		}
	}
	if r := call(t, path, map[string]any{"from": "2025-01-01", "to": "2026-10-16"}); r.status == http.StatusOK {
		t.Errorf("a period over 366 days was accepted")
	}
}
