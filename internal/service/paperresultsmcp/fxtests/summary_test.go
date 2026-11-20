package fxtests

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/fxdiag"
)

// seed records USD_JPY the way the recorder would have by 2026-10-18 12:00 JST
// (trading day 10-18 is still in progress, 10-17 is the latest complete one):
//
//	10-09 Fri recorded  minutes 0-4 (no gap)
//	10-10 Sat empty     market closed
//	10-11 Sun empty     market closed
//	10-12 Mon recorded  minutes 0,1,5,6 (an interior gap of 3 minutes: 2-4)
//	10-13 Tue no marker -> missing (never fetched or failed)
//	10-14 Wed recorded  minutes 0,10,11,20 (gaps of 9 and 8 minutes), 10 is BID only, 11 ASK only
//	10-15 Thu recorded  one minute
//	10-16 Fri no marker -> missing
//	10-17 Sat no marker -> missing (complete, not fetched yet)
//	10-18 Sun           in progress: neither recorded nor missing
func seedUSDJPY(t *testing.T, w *sql.DB) {
	t.Helper()
	for m := 0; m < 5; m++ {
		candle(t, w, "USD_JPY", "2026-10-09", m, "")
	}
	mark(t, w, "USD_JPY", "2026-10-09", "recorded", 5)
	mark(t, w, "USD_JPY", "2026-10-10", "empty", 0)
	mark(t, w, "USD_JPY", "2026-10-11", "empty", 0)
	for _, m := range []int{0, 1, 5, 6} {
		candle(t, w, "USD_JPY", "2026-10-12", m, "")
	}
	mark(t, w, "USD_JPY", "2026-10-12", "recorded", 4)
	for _, m := range []int{0, 10, 11, 20} {
		side := ""
		switch m {
		case 10:
			side = "bid"
		case 11:
			side = "ask"
		}
		candle(t, w, "USD_JPY", "2026-10-14", m, side)
	}
	mark(t, w, "USD_JPY", "2026-10-14", "recorded", 4)
	candle(t, w, "USD_JPY", "2026-10-15", 0, "")
	mark(t, w, "USD_JPY", "2026-10-15", "recorded", 1)
}

func seriesOf(t *testing.T, d fxdiag.Data, symbol string) fxdiag.Series {
	t.Helper()
	for _, s := range d.Series {
		if s.Symbol == symbol {
			return s
		}
	}
	t.Fatalf("no series %s in %+v", symbol, d.Series)
	return fxdiag.Series{}
}

func TestSummaryReportsCountsLastTimeDaysAndGaps(t *testing.T) {
	srv, w := newServer(t)
	seedUSDJPY(t, w)
	d := srv.call(t, nil).data(t)

	if d.From != "2026-09-19" || d.To != "2026-10-19" {
		t.Errorf("period = %s..%s, want the default 30 days", d.From, d.To)
	}
	if d.Notice == "" || len(d.Definitions) == 0 {
		t.Error("notice and definitions are always returned")
	}
	// Off by default; the configured pair is USD_JPY; lookback mirrors the recorder.
	if d.Recording.Enabled || !slices.Equal(d.Recording.Symbols, []string{"USD_JPY"}) || d.Recording.LookbackDays != 7 {
		t.Errorf("recording = %+v, want off, [USD_JPY], 7", d.Recording)
	}
	s := seriesOf(t, d, "USD_JPY")
	if !s.Configured || s.Candles != 14 || s.BidOnlyMinutes != 1 || s.AskOnlyMinutes != 1 {
		t.Errorf("series = candles %d bid-only %d ask-only %d configured %v; want 14, 1, 1, true", s.Candles, s.BidOnlyMinutes, s.AskOnlyMinutes, s.Configured)
	}
	if s.FirstOpenTime == nil || *s.FirstOpenTime != "2026-10-08T21:00:00Z" || s.LastOpenTime == nil || *s.LastOpenTime != "2026-10-14T21:00:00Z" {
		t.Errorf("first/last = %v / %v, want 2026-10-08T21:00:00Z / 2026-10-14T21:00:00Z", s.FirstOpenTime, s.LastOpenTime)
	}

	// Days: market-closed days (empty) are told from missing days (no marker).
	if s.Days.Recorded != 4 || s.Days.MarketClosed != 2 || !slices.Equal(s.Days.ClosedDays, []string{"2026-10-10", "2026-10-11"}) {
		t.Errorf("days = %+v, want 4 recorded and the weekend closed", s.Days)
	}
	if s.Days.Missing != 3 || !slices.Equal(s.Days.MissingDays, []string{"2026-10-13", "2026-10-16", "2026-10-17"}) {
		t.Errorf("missing = %d %v, want 10-13, 10-16, 10-17 (not the weekend, not 10-18 in progress, not days before the first marker)", s.Days.Missing, s.Days.MissingDays)
	}
	if s.Days.FirstMarkedDay == nil || *s.Days.FirstMarkedDay != "2026-10-09" || s.Days.RecordedThrough == nil || *s.Days.RecordedThrough != "2026-10-15" {
		t.Errorf("first marked %v, recorded through %v", s.Days.FirstMarkedDay, s.Days.RecordedThrough)
	}

	// Interior gaps: only inside a recorded day, between its first and last candle.
	g := s.InteriorGaps
	if g.Runs != 3 || g.MissingMinutes != 3+9+8 || g.Truncated || len(g.List) != 3 {
		t.Fatalf("gaps = %+v, want 3 runs of 3+9+8 minutes", g)
	}
	want := []fxdiag.GapRun{
		{TradingDay: "2026-10-12", Start: "2026-10-11T21:02:00Z", End: "2026-10-11T21:04:00Z", Minutes: 3},
		{TradingDay: "2026-10-14", Start: "2026-10-13T21:01:00Z", End: "2026-10-13T21:09:00Z", Minutes: 9},
		{TradingDay: "2026-10-14", Start: "2026-10-13T21:12:00Z", End: "2026-10-13T21:19:00Z", Minutes: 8},
	}
	if !slices.Equal(g.List, want) {
		t.Errorf("gap list = %+v\nwant %+v", g.List, want)
	}
}

// A pair with markers but not configured is listed; a configured pair with none is
// listed with zeros; the period and the symbol argument narrow the answer; the
// switch and the pairs come from runtime_settings.
func TestSummaryPeriodSymbolAndRecordingState(t *testing.T) {
	srv, w := newServer(t)
	seedUSDJPY(t, w)
	mark(t, w, "GBP_JPY", "2026-10-10", "empty", 0)
	for key, v := range map[string]string{"research.fx.enabled": "1", "research.fx.symbols": `"USD_JPY,EUR_USD"`} {
		if _, err := w.Exec(`INSERT INTO runtime_settings (key, value, updated_at) VALUES (?, ?, 't')`, key, v); err != nil {
			t.Fatal(err)
		}
	}
	d := srv.call(t, nil).data(t)
	if !d.Recording.Enabled || !slices.Equal(d.Recording.Symbols, []string{"USD_JPY", "EUR_USD"}) {
		t.Errorf("recording = %+v, want on with both pairs", d.Recording)
	}
	var names []string
	for _, s := range d.Series {
		names = append(names, s.Symbol)
	}
	if !slices.Equal(names, []string{"EUR_USD", "GBP_JPY", "USD_JPY"}) {
		t.Fatalf("series = %v, want every recorded or configured pair, sorted", names)
	}
	eur, gbp := seriesOf(t, d, "EUR_USD"), seriesOf(t, d, "GBP_JPY")
	if !eur.Configured || eur.Candles != 0 || eur.FirstOpenTime != nil || eur.Days.Missing != 0 || eur.Days.FirstMarkedDay != nil || eur.InteriorGaps.Runs != 0 {
		t.Errorf("configured but never recorded = %+v", eur)
	}
	if gbp.Configured || gbp.Days.MarketClosed != 1 || gbp.Days.Missing != 7 {
		t.Errorf("unconfigured pair with one closed day (10-10) = %+v, want 7 missing days (10-11..10-17)", gbp)
	}

	// The period narrows candles, days and gaps (trading days in [from, to)).
	d = srv.call(t, map[string]any{"from": "2026-10-12", "to": "2026-10-15", "symbol": "USD_JPY"}).data(t)
	if len(d.Series) != 1 {
		t.Fatalf("symbol filter returned %d series", len(d.Series))
	}
	s := d.Series[0]
	if s.Candles != 8 || s.Days.Recorded != 2 || s.Days.MarketClosed != 0 || s.Days.Missing != 1 || s.InteriorGaps.Runs != 3 {
		t.Errorf("period 10-12..10-15 = candles %d days %+v gaps %d; want 8 candles, 2 recorded, 1 missing (10-13), 3 gap runs", s.Candles, s.Days, s.InteriorGaps.Runs)
	}
}

// The gap list is bounded; the totals still cover every run.
func TestSummaryBoundsTheGapList(t *testing.T) {
	srv, w := newServer(t)
	n := 0
	for m := 0; m <= 2*(fxdiag.MaxGapRuns+10); m += 2 {
		candle(t, w, "USD_JPY", "2026-10-12", m, "")
		n++
	}
	mark(t, w, "USD_JPY", "2026-10-12", "recorded", n)
	g := seriesOf(t, srv.call(t, nil).data(t), "USD_JPY").InteriorGaps
	if g.Runs != fxdiag.MaxGapRuns+10 || g.MissingMinutes != fxdiag.MaxGapRuns+10 || len(g.List) != fxdiag.MaxGapRuns || !g.Truncated {
		t.Errorf("gaps = runs %d minutes %d listed %d truncated %v", g.Runs, g.MissingMinutes, len(g.List), g.Truncated)
	}
}

// A candle row without a marker (an interrupted day cannot exist, but a day whose
// marker is gone) does not make the day recorded: markers decide the day counts.
func TestSummaryCountsDaysByMarkersOnly(t *testing.T) {
	srv, w := newServer(t)
	candle(t, w, "USD_JPY", "2026-10-12", 0, "")
	mark(t, w, "USD_JPY", "2026-10-11", "empty", 0)
	s := seriesOf(t, srv.call(t, nil).data(t), "USD_JPY")
	if s.Candles != 1 || s.Days.Recorded != 0 || s.Days.MarketClosed != 1 || s.Days.Missing != 6 {
		t.Errorf("series = candles %d days %+v; want 1 candle, 0 recorded, 1 closed, 6 missing (10-12..10-17)", s.Candles, s.Days)
	}
}

func TestSummaryIsReadOnlyAndStrict(t *testing.T) {
	srv, w := newServer(t)
	seedUSDJPY(t, w)
	before := dump(t, w)
	srv.call(t, nil).data(t)
	srv.call(t, map[string]any{"symbol": "EUR_USD"}).data(t)
	srv.call(t, map[string]any{"unknown": 1}).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidArgument)
	for table, h := range dump(t, w) {
		if before[table] != h {
			t.Errorf("table %s changed during fx_series_summary", table)
		}
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
	for _, args := range []map[string]any{{"symbol": "usd_jpy"}, {"symbol": "USDJPY"}, {"symbol": 5}, {"symbol": strings.Repeat("A", 9)}} {
		srv.call(t, args).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidArgument)
	}
	srv.post(t, "tools/call", map[string]any{"name": toolName, "arguments": map[string]any{"from": "2026-10-01"}}).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidPeriod)
}
