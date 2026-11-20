package indextests

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/indexdiag"
)

// seedNikkei records 101 the way the daily task would have by 2026-10-18 (Sun)
// 12:00 JST:
//
//	10-09 Fri recorded    09:00-09:04 (no gap)
//	10-12 Mon             market holiday (Sports Day): no marker, not missing
//	10-13 Tue unavailable 立花 had no value
//	10-14 Wed no marker   -> missing
//	10-15 Thu recorded    09:00, 09:01, 09:05, 11:29, 12:30 (gaps 09:02-09:04 and 09:06-11:28; the lunch break is none)
//	10-16 Fri recorded    one value (15:29)
//	10-17 Sat             not a trading day: not missing
func seedNikkei(t *testing.T, w *sql.DB) {
	t.Helper()
	for _, hm := range []string{"09:00", "09:01", "09:02", "09:03", "09:04"} {
		value(t, w, "101", "2026-10-09", hm, 38000)
	}
	mark(t, w, "101", "2026-10-09", "recorded", 5)
	mark(t, w, "101", "2026-10-13", "unavailable", 0)
	for _, hm := range []string{"09:00", "09:01", "09:05", "11:29", "12:30"} {
		value(t, w, "101", "2026-10-15", hm, 38100)
	}
	mark(t, w, "101", "2026-10-15", "recorded", 5)
	value(t, w, "101", "2026-10-16", "15:29", 38200)
	mark(t, w, "101", "2026-10-16", "recorded", 1)
}

func seriesOf(t *testing.T, d indexdiag.Data, symbol string) indexdiag.Series {
	t.Helper()
	for _, s := range d.Series {
		if s.Symbol == symbol {
			return s
		}
	}
	t.Fatalf("no series %s in %+v", symbol, d.Series)
	return indexdiag.Series{}
}

func TestSummaryReportsCountsLastTimeDaysAndSessionGaps(t *testing.T) {
	srv, w := newServer(t)
	seedNikkei(t, w)
	source(t, w, "101", "2026-10-09", "09:00")
	source(t, w, "101", "2026-10-15", "09:00")
	source(t, w, "7203", "2026-10-15", "09:00") // another symbol: not counted
	d := srv.call(t, nil).data(t)

	if d.From != "2026-09-19" || d.To != "2026-10-19" || d.Notice == "" || len(d.Definitions) == 0 {
		t.Errorf("period = %s..%s, notice %q, %d definitions", d.From, d.To, d.Notice, len(d.Definitions))
	}
	// Off by default; the configured codes are 101 and 151; lookback mirrors the recorder.
	if d.Recording.Enabled || !slices.Equal(d.Recording.Symbols, []string{"101", "151"}) || d.Recording.LookbackDays != 5 {
		t.Errorf("recording = %+v, want off, [101 151], 5", d.Recording)
	}
	s := seriesOf(t, d, "101")
	if !s.Configured || s.Name != "日経平均" || s.Minutes != 11 || s.SourceSamples != 2 {
		t.Errorf("series = %+v; want configured, 日経平均, 11 minutes, 2 source samples", s)
	}
	if s.FirstOpenTime == nil || *s.FirstOpenTime != "2026-10-09T00:00:00Z" || s.LastOpenTime == nil || *s.LastOpenTime != "2026-10-16T06:29:00Z" {
		t.Errorf("first/last = %v / %v", s.FirstOpenTime, s.LastOpenTime)
	}
	if s.Days.Recorded != 3 || s.Days.Unavailable != 1 || !slices.Equal(s.Days.UnavailableDays, []string{"2026-10-13"}) {
		t.Errorf("days = %+v, want 3 recorded and 10-13 unavailable", s.Days)
	}
	if s.Days.Missing != 1 || !slices.Equal(s.Days.MissingDays, []string{"2026-10-14"}) {
		t.Errorf("missing = %d %v, want only 10-14 (not the holiday 10-12, not the Saturday)", s.Days.Missing, s.Days.MissingDays)
	}
	if s.Days.FirstMarkedDay == nil || *s.Days.FirstMarkedDay != "2026-10-09" || s.Days.RecordedThrough == nil || *s.Days.RecordedThrough != "2026-10-16" {
		t.Errorf("first marked %v, recorded through %v", s.Days.FirstMarkedDay, s.Days.RecordedThrough)
	}
	want := []indexdiag.GapRun{
		{TradingDay: "2026-10-15", Start: "2026-10-15T00:02:00Z", End: "2026-10-15T00:04:00Z", Minutes: 3},
		{TradingDay: "2026-10-15", Start: "2026-10-15T00:06:00Z", End: "2026-10-15T02:28:00Z", Minutes: 143},
	}
	if g := s.InteriorGaps; g.Runs != 2 || g.MissingMinutes != 146 || g.Truncated || !slices.Equal(g.List, want) {
		t.Errorf("gaps = %+v, want %+v (the lunch break 11:30-12:29 is no gap)", g, want)
	}
	// 151 is configured but has nothing: listed with zeros.
	if z := seriesOf(t, d, "151"); !z.Configured || z.Name != "TOPIX" || z.Minutes != 0 || z.FirstOpenTime != nil || z.Days.FirstMarkedDay != nil || z.Days.Missing != 0 {
		t.Errorf("151 = %+v", z)
	}
}

// A session that is not complete yet (before 15:35 JST) is neither recorded nor
// missing; from 15:35 it is missing when it has no marker.
func TestSummaryCountsTodayMissingOnlyAfterTheSessionIsComplete(t *testing.T) {
	_, w := newServer(t)
	mark(t, w, "101", "2026-10-15", "recorded", 1)
	value(t, w, "101", "2026-10-15", "09:00", 1)
	q := w
	for _, c := range []struct {
		now     string
		missing []string
	}{
		{"2026-10-16 15:34", []string{}},
		{"2026-10-16 15:35", []string{"2026-10-16"}},
	} {
		d, _, err := indexdiag.Summarize(t.Context(), q, rangeTo("2026-10-01", "2026-10-31"), at(strings.Fields(c.now)[0], strings.Fields(c.now)[1]), "101")
		if err != nil {
			t.Fatal(err)
		}
		if got := d.Series[0].Days.MissingDays; !slices.Equal(got, c.missing) {
			t.Errorf("now %s: missing = %v, want %v", c.now, got, c.missing)
		}
	}
}

func TestSummaryPeriodSymbolAndRecordingState(t *testing.T) {
	srv, w := newServer(t)
	seedNikkei(t, w)
	mark(t, w, "102", "2026-10-13", "unavailable", 0)
	for key, v := range map[string]string{"research.index.enabled": "1", "research.index.symbols": `"101"`} {
		if _, err := w.Exec(`INSERT INTO runtime_settings (key, value, updated_at) VALUES (?, ?, 't')`, key, v); err != nil {
			t.Fatal(err)
		}
	}
	d := srv.call(t, nil).data(t)
	if !d.Recording.Enabled || !slices.Equal(d.Recording.Symbols, []string{"101"}) {
		t.Errorf("recording = %+v, want on with 101", d.Recording)
	}
	var names []string
	for _, s := range d.Series {
		names = append(names, s.Symbol)
	}
	if !slices.Equal(names, []string{"101", "102"}) {
		t.Fatalf("series = %v, want every recorded or configured code, sorted", names)
	}
	if z := seriesOf(t, d, "102"); z.Configured || z.Name != "" || z.Days.Unavailable != 1 {
		t.Errorf("102 = %+v", z)
	}
	d = srv.call(t, map[string]any{"from": "2026-10-15", "to": "2026-10-16", "symbol": "101"}).data(t)
	if len(d.Series) != 1 || d.Series[0].Minutes != 5 || d.Series[0].Days.Recorded != 1 || d.Series[0].InteriorGaps.Runs != 2 {
		t.Errorf("period 10-15 only = %+v", d.Series)
	}
}

// The tool reads only: no table changes, an unknown argument or a malformed code
// is invalid_argument, and the schema is the read-only one.
func TestSummaryIsReadOnlyAndStrict(t *testing.T) {
	srv, w := newServer(t)
	seedNikkei(t, w)
	before := dump(t, w)
	srv.call(t, nil).data(t)
	srv.call(t, map[string]any{"symbol": "151"}).data(t)
	srv.call(t, map[string]any{"unknown": 1}).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidArgument)
	for table, h := range dump(t, w) {
		if before[table] != h {
			t.Errorf("table %s changed during %s", table, toolName)
		}
	}
	for _, args := range []map[string]any{{"symbol": "1 01"}, {"symbol": "101,151"}, {"symbol": 5}, {"symbol": strings.Repeat("1", 9)}} {
		srv.call(t, args).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidArgument)
	}

	var env struct {
		Result struct {
			Tools []struct {
				Name        string         `json:"name"`
				Annotations map[string]any `json:"annotations"`
				InputSchema struct {
					Properties           map[string]any `json:"properties"`
					AdditionalProperties bool           `json:"additionalProperties"`
				} `json:"inputSchema"`
			} `json:"tools"`
		} `json:"result"`
	}
	r := srv.post(t, "tools/list", nil)
	if err := json.Unmarshal(r.body, &env); err != nil || r.status != http.StatusOK {
		t.Fatalf("tools/list: %d %s %v", r.status, r.body, err)
	}
	found := false
	for _, tl := range env.Result.Tools {
		if tl.Name != toolName {
			continue
		}
		found = true
		if tl.Annotations["readOnlyHint"] != true || tl.Annotations["destructiveHint"] != false || tl.InputSchema.AdditionalProperties {
			t.Errorf("annotations %v, schema %+v", tl.Annotations, tl.InputSchema)
		}
		var props []string
		for p := range tl.InputSchema.Properties {
			props = append(props, p)
		}
		slices.Sort(props)
		if strings.Join(props, ",") != "from,symbol,to" {
			t.Errorf("arguments = %v", props)
		}
	}
	if !found {
		t.Fatalf("%s is not in tools/list", toolName)
	}
}
