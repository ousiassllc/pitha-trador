package gmo

import (
	"context"
	"errors"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

var (
	// Monday 2023-10-30 07:30 JST, inside GMO trading day 20231030.
	monday = time.Date(2023, time.October, 30, 7, 30, 0, 0, jst)
)

func TestKlines(t *testing.T) {
	ms := func(day, hour, min int) time.Time {
		return time.Date(2023, time.October, day, hour, min, 0, 0, jst).UTC()
	}
	tests := []struct {
		name    string
		req     KlinesRequest
		fixture string
		wantURI string
		want    []Candle
	}{
		{
			name:    "1min BID",
			req:     KlinesRequest{Symbol: "USD_JPY", PriceType: PriceBid, Interval: Interval1Min, At: monday},
			fixture: "klines_1min_bid.json",
			wantURI: "/v1/klines?date=20231030&interval=1min&priceType=BID&symbol=USD_JPY",
			want: []Candle{
				{OpenTime: ms(30, 6, 0), Open: 149.805, High: 149.811, Low: 149.801, Close: 149.808},
				{OpenTime: ms(30, 6, 1), Open: 149.808, High: 149.815, Low: 149.806, Close: 149.812},
				{OpenTime: ms(30, 6, 2), Open: 149.812, High: 149.813, Low: 149.804, Close: 149.806},
			},
		},
		{
			name:    "1min ASK",
			req:     KlinesRequest{Symbol: "USD_JPY", PriceType: PriceAsk, Interval: Interval1Min, At: monday},
			fixture: "klines_1min_ask.json",
			wantURI: "/v1/klines?date=20231030&interval=1min&priceType=ASK&symbol=USD_JPY",
			want: []Candle{
				{OpenTime: ms(30, 6, 0), Open: 149.809, High: 149.815, Low: 149.805, Close: 149.812},
				{OpenTime: ms(30, 6, 1), Open: 149.812, High: 149.819, Low: 149.810, Close: 149.816},
				{OpenTime: ms(30, 6, 2), Open: 149.816, High: 149.817, Low: 149.808, Close: 149.810},
			},
		},
		{
			name:    "1day BID",
			req:     KlinesRequest{Symbol: "USD_JPY", PriceType: PriceBid, Interval: Interval1Day, At: monday},
			fixture: "klines_1day_bid.json",
			wantURI: "/v1/klines?date=2023&interval=1day&priceType=BID&symbol=USD_JPY",
			want: []Candle{
				{OpenTime: ms(26, 6, 0), Open: 149.742, High: 150.162, Low: 149.470, Close: 149.841},
				{OpenTime: ms(27, 6, 0), Open: 149.841, High: 149.934, Low: 149.021, Close: 149.108},
				{OpenTime: ms(30, 6, 0), Open: 149.805, High: 150.145, Low: 149.605, Close: 150.087},
			},
		},
		{
			name:    "1day ASK",
			req:     KlinesRequest{Symbol: "USD_JPY", PriceType: PriceAsk, Interval: Interval1Day, At: monday},
			fixture: "klines_1day_ask.json",
			wantURI: "/v1/klines?date=2023&interval=1day&priceType=ASK&symbol=USD_JPY",
			want: []Candle{
				{OpenTime: ms(26, 6, 0), Open: 149.746, High: 150.166, Low: 149.474, Close: 149.845},
				{OpenTime: ms(27, 6, 0), Open: 149.845, High: 149.938, Low: 149.025, Close: 149.112},
				{OpenTime: ms(30, 6, 0), Open: 149.809, High: 150.149, Low: 149.609, Close: 150.091},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clk := newFakeClock()
			srv := newFakeServer(t, clk, step{fixture: tt.fixture})
			c, _ := testClient(t, srv, clk, Config{})

			got, err := c.Klines(context.Background(), tt.req)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("candles =\n%+v\nwant\n%+v", got, tt.want)
			}
			if reqs := srv.requests(); len(reqs) != 1 || reqs[0] != tt.wantURI {
				t.Fatalf("requests = %v, want [%s]", reqs, tt.wantURI)
			}
		})
	}
}

func TestKlinesBidIsBelowAsk(t *testing.T) {
	clk := newFakeClock()
	srv := newFakeServer(t, clk, step{fixture: "klines_1min_bid.json"}, step{fixture: "klines_1min_ask.json"})
	c, _ := testClient(t, srv, clk, Config{})
	req := KlinesRequest{Symbol: "USD_JPY", Interval: Interval1Min, At: monday}

	req.PriceType = PriceBid
	bid, err := c.Klines(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	req.PriceType = PriceAsk
	ask, err := c.Klines(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if len(bid) != len(ask) {
		t.Fatalf("BID has %d candles, ASK %d", len(bid), len(ask))
	}
	for i := range bid {
		if !bid[i].OpenTime.Equal(ask[i].OpenTime) || bid[i].Close >= ask[i].Close {
			t.Fatalf("candle %d: bid %+v ask %+v", i, bid[i], ask[i])
		}
	}
}

func TestKlinesEmptyDayIsNotAnError(t *testing.T) {
	clk := newFakeClock()
	srv := newFakeServer(t, clk, step{fixture: "klines_empty.json"})
	c, _ := testClient(t, srv, clk, Config{})

	got, err := c.Klines(context.Background(), KlinesRequest{Symbol: "USD_JPY", PriceType: PriceBid, Interval: Interval1Min, At: monday})
	if err != nil || len(got) != 0 {
		t.Fatalf("got %v, %v; want no candles, no error", got, err)
	}
}

func TestKlinesRejectsUnsupportedRequestsWithoutCalling(t *testing.T) {
	valid := KlinesRequest{Symbol: "USD_JPY", PriceType: PriceBid, Interval: Interval1Min, At: monday}
	with := func(f func(*KlinesRequest)) KlinesRequest { r := valid; f(&r); return r }
	tests := []struct {
		name string
		req  KlinesRequest
		want error
	}{
		{"5min interval", with(func(r *KlinesRequest) { r.Interval = "5min" }), ErrUnsupportedInterval},
		{"1hour interval", with(func(r *KlinesRequest) { r.Interval = "1hour" }), ErrUnsupportedInterval},
		{"empty interval", with(func(r *KlinesRequest) { r.Interval = "" }), ErrUnsupportedInterval},
		{"unconfigured symbol", with(func(r *KlinesRequest) { r.Symbol = "EUR_JPY" }), ErrUnsupportedSymbol},
		{"lowercase symbol", with(func(r *KlinesRequest) { r.Symbol = "usd_jpy" }), ErrUnsupportedSymbol},
		{"price type", with(func(r *KlinesRequest) { r.PriceType = "MID" }), ErrUnsupportedPriceType},
		{"empty price type", with(func(r *KlinesRequest) { r.PriceType = "" }), ErrUnsupportedPriceType},
		{"1day before the first supported year", with(func(r *KlinesRequest) { r.Interval = Interval1Day; r.At = time.Date(2022, 6, 1, 12, 0, 0, 0, jst) }), ErrUnsupportedDate},
		{"1min before 2023-10-28", with(func(r *KlinesRequest) { r.At = time.Date(2023, 10, 27, 12, 0, 0, 0, jst) }), ErrUnsupportedDate},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clk := newFakeClock()
			srv := newFakeServer(t, clk)
			c, _ := testClient(t, srv, clk, Config{})

			_, err := c.Klines(context.Background(), tt.req)
			if !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
			if n := len(srv.requests()); n != 0 {
				t.Fatalf("%d calls, want 0", n)
			}
		})
	}
}

func TestKlinesDateParam(t *testing.T) {
	jstAt := func(y int, mo time.Month, d, h, mi int) time.Time { return time.Date(y, mo, d, h, mi, 0, 0, jst) }
	tests := []struct {
		name     string
		interval Interval
		at       time.Time
		want     string
		wantErr  error
	}{
		{"1min mid-day", Interval1Min, jstAt(2023, 10, 30, 7, 30), "20231030", nil},
		{"1min before the 06:00 switch belongs to the previous day", Interval1Min, jstAt(2023, 10, 31, 5, 59), "20231030", nil},
		{"1min at the 06:00 switch", Interval1Min, jstAt(2023, 10, 31, 6, 0), "20231031", nil},
		{"1min first available trading day", Interval1Min, jstAt(2023, 10, 28, 6, 0), "20231028", nil},
		{"1min just before the first trading day", Interval1Min, jstAt(2023, 10, 28, 5, 59), "", ErrUnsupportedDate},
		{"1min given in UTC is converted to JST", Interval1Min, time.Date(2023, 10, 30, 20, 30, 0, 0, time.UTC), "20231030", nil},
		{"1day year", Interval1Day, jstAt(2023, 10, 30, 7, 30), "2023", nil},
		{"1day first supported year", Interval1Day, jstAt(2023, 1, 1, 0, 0), "2023", nil},
		{"1day year before the first supported year", Interval1Day, jstAt(2022, 12, 31, 23, 59), "", ErrUnsupportedDate},
		{"1day year is judged in JST, not UTC", Interval1Day, time.Date(2022, 12, 31, 15, 0, 0, 0, time.UTC), "2023", nil},
		{"1day year follows JST", Interval1Day, time.Date(2023, 12, 31, 20, 0, 0, 0, time.UTC), "2024", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := KlinesRequest{Interval: tt.interval, At: tt.at}.dateParam()
			if !errors.Is(err, tt.wantErr) || got != tt.want {
				t.Fatalf("dateParam = %q, %v; want %q, %v", got, err, tt.want, tt.wantErr)
			}
		})
	}
}

func TestKlinesInvalidCandlesAreNotRetried(t *testing.T) {
	tests := []struct{ name, body string }{
		{"bad openTime", `{"status":0,"data":[{"openTime":"soon","open":"1","high":"1","low":"1","close":"1"}]}`},
		{"empty price", `{"status":0,"data":[{"openTime":"1698613200000","open":"","high":"1","low":"1","close":"1"}]}`},
		{"negative price", `{"status":0,"data":[{"openTime":"1698613200000","open":"1","high":"1","low":"-1","close":"1"}]}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clk := newFakeClock()
			srv := newFakeServer(t, clk, step{body: tt.body})
			c, _ := testClient(t, srv, clk, Config{})

			_, err := c.Klines(context.Background(), KlinesRequest{Symbol: "USD_JPY", PriceType: PriceBid, Interval: Interval1Min, At: monday})
			if !errors.Is(err, ErrInvalidResponse) || len(srv.requests()) != 1 {
				t.Fatalf("err = %v, calls = %d; want ErrInvalidResponse after 1 call", err, len(srv.requests()))
			}
		})
	}
}

// GMO's FX public API returns no volume or order book, so this package must
// not be built on internal/service/broker's Quote (Volume, BidDepth). This
// keeps the dependency from creeping in, and the types free of volume fields.
func TestDoesNotDependOnBrokerQuoteOrVolume(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, f := range files {
		parsed, err := parser.ParseFile(fset, f, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range parsed.Imports {
			path, _ := strconv.Unquote(imp.Path.Value)
			if strings.Contains(path, "internal/service/broker") {
				t.Errorf("%s imports %s", f, path)
			}
		}
	}
	for _, typ := range []reflect.Type{reflect.TypeFor[Ticker](), reflect.TypeFor[Candle]()} {
		for i := range typ.NumField() {
			if name := typ.Field(i).Name; strings.Contains(name, "Volume") || strings.Contains(name, "Depth") {
				t.Errorf("%s has field %s", typ.Name(), name)
			}
		}
	}
}
