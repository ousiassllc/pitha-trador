package researchfx_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	rfx "github.com/ousiassllc/pitha-trador/internal/domain/researchfx"
	"github.com/ousiassllc/pitha-trador/internal/service/fx/gmo"
	"github.com/ousiassllc/pitha-trador/internal/service/researchfx"
)

var jst = rfx.JST

// at is a JST wall-clock time.
func at(day string, hour, min int) time.Time {
	d, err := time.ParseInLocation(time.DateOnly, day, jst)
	if err != nil {
		panic(err)
	}
	return d.Add(time.Duration(hour)*time.Hour + time.Duration(min)*time.Minute)
}

type memStore struct {
	marked map[string]map[string]bool
	record []string
	fail   error
}

func newStore() *memStore { return &memStore{marked: map[string]map[string]bool{}} }

func (s *memStore) MarkedDays(_ context.Context, symbol, from, to string) (map[string]bool, error) {
	out := map[string]bool{}
	for d := range s.marked[symbol] {
		if d >= from && d <= to {
			out[d] = true
		}
	}
	return out, nil
}

func (s *memStore) RecordDay(_ context.Context, symbol, day string, candles []rfx.Candle, _ time.Time) (rfx.Day, error) {
	if s.fail != nil {
		return rfx.Day{}, s.fail
	}
	if s.marked[symbol] == nil {
		s.marked[symbol] = map[string]bool{}
	}
	s.marked[symbol][day] = true
	s.record = append(s.record, symbol+" "+day)
	st := rfx.StatusRecorded
	if len(candles) == 0 {
		st = rfx.StatusEmpty
	}
	return rfx.Day{Symbol: symbol, TradingDay: day, Status: st, CandleCount: len(candles)}, nil
}

type call struct {
	symbol string
	price  gmo.PriceType
	day    string
}

type fakeFetcher struct {
	calls   []call
	failDay map[string]bool
	candles map[gmo.PriceType][]gmo.Candle
}

func (f *fakeFetcher) Klines(_ context.Context, req gmo.KlinesRequest) ([]gmo.Candle, error) {
	day := rfx.TradingDay(req.At)
	f.calls = append(f.calls, call{req.Symbol, req.PriceType, day})
	if req.Interval != gmo.Interval1Min {
		return nil, errors.New("not a 1min request")
	}
	if f.failDay[day] {
		return nil, errors.New("boom")
	}
	return f.candles[req.PriceType], nil
}

func newRecorder(st *memStore, f *fakeFetcher, now time.Time, symbols ...string) *researchfx.Recorder {
	return researchfx.NewRecorder(st, func(string) (researchfx.Fetcher, error) { return f, nil },
		func(context.Context) ([]string, error) { return symbols, nil }).
		WithClock(func() time.Time { return now }).WithPace(0, nil)
}

func TestWindow_IsTheCompletedTradingDaysOfTheLookback(t *testing.T) {
	// Friday 2026-10-16 05:59 JST is still trading day 10-15; 06:00 starts 10-16.
	got := researchfx.Window(at("2026-10-16", 5, 59), 3)
	if want := []string{"2026-10-12", "2026-10-13", "2026-10-14"}; !slices.Equal(got, want) {
		t.Errorf("Window at 05:59 = %v, want %v", got, want)
	}
	got = researchfx.Window(at("2026-10-16", 6, 0), 3)
	if want := []string{"2026-10-13", "2026-10-14", "2026-10-15"}; !slices.Equal(got, want) {
		t.Errorf("Window at 06:00 = %v, want %v", got, want)
	}
	// Never earlier than the first day GMO serves 1min candles for (2023-10-28).
	got = researchfx.Window(at("2023-10-30", 12, 0), 7)
	if want := []string{"2023-10-28", "2023-10-29"}; !slices.Equal(got, want) {
		t.Errorf("Window near the start of the data = %v, want %v", got, want)
	}
}

func TestMerge_JoinsBidAndAskByOpenTimeAndKeepsOneSidedMinutes(t *testing.T) {
	t0 := at("2026-10-15", 6, 0).UTC()
	bid := []gmo.Candle{{OpenTime: t0, Open: 1, High: 2, Low: 0.5, Close: 1.5}, {OpenTime: t0.Add(time.Minute), Open: 3, High: 3, Low: 3, Close: 3}}
	ask := []gmo.Candle{{OpenTime: t0.Add(2 * time.Minute), Open: 9, High: 9, Low: 9, Close: 9}, {OpenTime: t0, Open: 1.1, High: 2.1, Low: 0.6, Close: 1.6}}
	got := researchfx.Merge("USD_JPY", "2026-10-15", bid, ask)
	if len(got) != 3 {
		t.Fatalf("Merge = %d candles, want 3: %+v", len(got), got)
	}
	if got[0].Bid == nil || got[0].Ask == nil || got[0].Bid.Close != 1.5 || got[0].Ask.Close != 1.6 {
		t.Errorf("minute 0 not merged: %+v", got[0])
	}
	if got[1].Bid == nil || got[1].Ask != nil {
		t.Errorf("minute 1 (BID only) = %+v", got[1])
	}
	if got[2].Bid != nil || got[2].Ask == nil || !got[2].OpenTime.Equal(t0.Add(2*time.Minute)) {
		t.Errorf("minute 2 (ASK only, sorted last) = %+v", got[2])
	}
	for _, c := range got {
		if c.Symbol != "USD_JPY" || c.TradingDay != "2026-10-15" {
			t.Errorf("candle %+v lost its symbol or trading day", c)
		}
	}
}

// A Run is one (pair, day): the oldest unmarked one, two klines requests (BID
// then ASK), and More while others are pending; today's trading day is never
// requested.
func TestRun_RecordsTheOldestUnmarkedDayWithABidAndAnAskRequest(t *testing.T) {
	st, f := newStore(), &fakeFetcher{candles: map[gmo.PriceType][]gmo.Candle{
		gmo.PriceBid: {{OpenTime: at("2026-10-13", 6, 0).UTC(), Open: 1, High: 1, Low: 1, Close: 1}},
	}}
	st.marked["USD_JPY"] = map[string]bool{"2026-10-13": true}
	rec := newRecorder(st, f, at("2026-10-16", 10, 0), "USD_JPY").WithLookbackDays(4) // 10-12..10-15
	res, err := rec.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Day != "2026-10-12" || res.Symbol != "USD_JPY" || !res.More || res.Status != rfx.StatusRecorded || res.Candles != 1 {
		t.Fatalf("Run = %+v, want 2026-10-12 recorded with More", res)
	}
	want := []call{{"USD_JPY", gmo.PriceBid, "2026-10-12"}, {"USD_JPY", gmo.PriceAsk, "2026-10-12"}}
	if !slices.Equal(f.calls, want) {
		t.Fatalf("requests = %v, want %v", f.calls, want)
	}
	// Two more Runs finish 10-14 and 10-15; the last one has nothing left.
	for _, wantMore := range []bool{true, false} {
		if res, err = rec.Run(context.Background()); err != nil || res.More != wantMore {
			t.Fatalf("Run = %+v, %v; want More %v", res, err, wantMore)
		}
	}
	if len(f.calls) != 6 {
		t.Fatalf("requests after 3 Runs = %d, want 6 (two per day)", len(f.calls))
	}
	// Everything is marked: a further Run makes no request.
	if res, err = rec.Run(context.Background()); err != nil || res.Day != "" || res.More {
		t.Fatalf("idle Run = %+v, %v", res, err)
	}
	if len(f.calls) != 6 {
		t.Fatalf("an idle Run made requests: %v", f.calls[6:])
	}
	for _, c := range f.calls {
		if c.day == "2026-10-16" {
			t.Fatalf("today's trading day was requested: %+v", c)
		}
	}
}

// A day whose fetch fails gets no marker and the error is returned; the next Run
// does the other days first (one refused day does not starve the rest) and retries
// the failed one last.
func TestRun_AFailedFetchLeavesNoMarkerAndDoesNotBlockLaterDays(t *testing.T) {
	st, f := newStore(), &fakeFetcher{failDay: map[string]bool{"2026-10-12": true}}
	rec := newRecorder(st, f, at("2026-10-15", 10, 0), "USD_JPY").WithLookbackDays(3) // 10-12..10-14
	if _, err := rec.Run(context.Background()); err == nil {
		t.Fatal("a failed fetch was not reported")
	}
	if len(st.record) != 0 {
		t.Fatalf("a marker was written for a failed day: %v", st.record)
	}
	for range 2 {
		if _, err := rec.Run(context.Background()); err != nil {
			t.Fatalf("the days behind the failed one: %v", err)
		}
	}
	if want := []string{"USD_JPY 2026-10-13", "USD_JPY 2026-10-14"}; !slices.Equal(st.record, want) {
		t.Fatalf("recorded %v, want %v", st.record, want)
	}
	f.failDay = nil // GMO answers again: the failed day is retried and marked.
	if res, err := rec.Run(context.Background()); err != nil || res.Day != "2026-10-12" || res.More {
		t.Fatalf("retry = %+v, %v", res, err)
	}
}

func TestRun_AStoreFailureLeavesTheDayUnmarked(t *testing.T) {
	st, f := newStore(), &fakeFetcher{}
	st.fail = errors.New("disk full")
	rec := newRecorder(st, f, at("2026-10-16", 10, 0), "USD_JPY").WithLookbackDays(1)
	if _, err := rec.Run(context.Background()); !errors.Is(err, st.fail) {
		t.Fatalf("Run = %v, want the store error", err)
	}
	if len(st.record) != 0 {
		t.Fatal("marked despite the failed write")
	}
}

// Pairs are visited oldest day first, in the configured order, one unit per Run.
func TestRun_CoversEveryConfiguredPair(t *testing.T) {
	st, f := newStore(), &fakeFetcher{}
	rec := newRecorder(st, f, at("2026-10-16", 10, 0), "USD_JPY", "EUR_USD").WithLookbackDays(1)
	for range 2 {
		if _, err := rec.Run(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if want := []string{"USD_JPY 2026-10-15", "EUR_USD 2026-10-15"}; !slices.Equal(st.record, want) {
		t.Fatalf("recorded %v, want %v", st.record, want)
	}
}

// Between two Runs the Recorder waits so the adapter's one-call-per-second limit
// also holds across the per-pair clients; the first Run does not wait.
func TestRun_PacesConsecutiveRuns(t *testing.T) {
	var waits []time.Duration
	st, f := newStore(), &fakeFetcher{}
	rec := researchfx.NewRecorder(st, func(string) (researchfx.Fetcher, error) { return f, nil },
		func(context.Context) ([]string, error) { return []string{"USD_JPY"}, nil }).
		WithClock(func() time.Time { return at("2026-10-16", 10, 0) }).WithLookbackDays(3).
		WithPace(time.Second, func(_ context.Context, d time.Duration) error { waits = append(waits, d); return nil })
	for range 3 {
		if _, err := rec.Run(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if len(waits) != 2 {
		t.Fatalf("waits = %v, want one before each Run after the first", waits)
	}
	for _, w := range waits {
		if w <= 0 || w > time.Second {
			t.Errorf("wait %v is not within (0, 1s]", w)
		}
	}
}
