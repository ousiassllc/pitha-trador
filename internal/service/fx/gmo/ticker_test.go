package gmo

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestTickerUSDJPY(t *testing.T) {
	clk := newFakeClock()
	srv := newFakeServer(t, clk, step{fixture: "ticker.json"})
	c, _ := testClient(t, srv, clk, Config{})

	got, err := c.Ticker(context.Background(), "USD_JPY")
	if err != nil {
		t.Fatal(err)
	}
	want := Ticker{
		Symbol:    "USD_JPY",
		Bid:       149.808,
		Ask:       149.812,
		Timestamp: time.Date(2023, time.October, 30, 21, 2, 11, 123456000, time.UTC),
		Status:    MarketOpen,
	}
	if got != want {
		t.Fatalf("Ticker = %+v, want %+v", got, want)
	}
	if reqs := srv.requests(); len(reqs) != 1 || reqs[0] != "/v1/ticker" {
		t.Fatalf("requests = %v, want one GET /v1/ticker", reqs)
	}
}

func TestTickersReturnsConfiguredPairsInOrderFromOneCall(t *testing.T) {
	clk := newFakeClock()
	srv := newFakeServer(t, clk, step{fixture: "ticker.json"})
	c, _ := testClient(t, srv, clk, Config{Symbols: []string{"GBP_JPY", "USD_JPY"}})

	got, err := c.Tickers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Symbol != "GBP_JPY" || got[1].Symbol != "USD_JPY" {
		t.Fatalf("Tickers = %+v, want GBP_JPY then USD_JPY (EUR_JPY is not configured)", got)
	}
	if got[0].Status != MarketClose || got[1].Status != MarketOpen {
		t.Fatalf("statuses = %s, %s; want CLOSE, OPEN", got[0].Status, got[1].Status)
	}
	if n := len(srv.requests()); n != 1 {
		t.Fatalf("%d calls, want 1 for all pairs", n)
	}
}

func TestTickerRejectsUnconfiguredSymbolWithoutCalling(t *testing.T) {
	clk := newFakeClock()
	srv := newFakeServer(t, clk)
	c, _ := testClient(t, srv, clk, Config{})

	_, err := c.Ticker(context.Background(), "EUR_JPY")
	if !errors.Is(err, ErrUnsupportedSymbol) {
		t.Fatalf("err = %v, want ErrUnsupportedSymbol", err)
	}
	if n := len(srv.requests()); n != 0 {
		t.Fatalf("%d calls, want 0", n)
	}
}

func TestTickerInvalidResponsesAreNotRetried(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"symbol missing", `{"status":0,"data":[{"symbol":"EUR_JPY","ask":"1","bid":"1","timestamp":"2023-10-30T21:02:11.1Z","status":"OPEN"}]}`},
		{"not json", `<html>oops</html>`},
		{"zero price", `{"status":0,"data":[{"symbol":"USD_JPY","ask":"0","bid":"149.8","timestamp":"2023-10-30T21:02:11.1Z","status":"OPEN"}]}`},
		{"unparsable price", `{"status":0,"data":[{"symbol":"USD_JPY","ask":"x","bid":"149.8","timestamp":"2023-10-30T21:02:11.1Z","status":"OPEN"}]}`},
		{"bad timestamp", `{"status":0,"data":[{"symbol":"USD_JPY","ask":"149.9","bid":"149.8","timestamp":"yesterday","status":"OPEN"}]}`},
		{"unknown market status", `{"status":0,"data":[{"symbol":"USD_JPY","ask":"149.9","bid":"149.8","timestamp":"2023-10-30T21:02:11.1Z","status":"HALT"}]}`},
		{"data is not a list", `{"status":0,"data":{"symbol":"USD_JPY"}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clk := newFakeClock()
			srv := newFakeServer(t, clk, step{body: tt.body})
			c, _ := testClient(t, srv, clk, Config{})

			_, err := c.Ticker(context.Background(), "USD_JPY")
			if !errors.Is(err, ErrInvalidResponse) {
				t.Fatalf("err = %v, want ErrInvalidResponse", err)
			}
			if errors.Is(err, ErrRetriesExhausted) || len(srv.requests()) != 1 {
				t.Fatalf("invalid response must not be retried (calls = %d)", len(srv.requests()))
			}
		})
	}
}

func TestTickerStatusClosedStillReturnsQuote(t *testing.T) {
	clk := newFakeClock()
	srv := newFakeServer(t, clk, step{fixture: "ticker.json"})
	c, _ := testClient(t, srv, clk, Config{Symbols: []string{"GBP_JPY"}})

	got, err := c.Ticker(context.Background(), "GBP_JPY")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != MarketClose || got.Bid != 181.918 || got.Ask != 181.940 {
		t.Fatalf("Ticker = %+v", got)
	}
}

// An all-pairs response carries every pair GMO lists; only the configured
// ones are validated, so a bad row for another pair must not break USD_JPY.
func TestTickerIgnoresInvalidRowsOfUnconfiguredPairs(t *testing.T) {
	const usd = `{"symbol":"USD_JPY","ask":"149.812","bid":"149.808","timestamp":"2023-10-30T21:02:11.123456Z","status":"OPEN"}`
	tests := []struct{ name, extra string }{
		{"zero price", `{"symbol":"EUR_JPY","ask":"0","bid":"0","timestamp":"2023-10-30T21:02:10Z","status":"OPEN"}`},
		{"unknown status", `{"symbol":"EUR_JPY","ask":"1","bid":"1","timestamp":"2023-10-30T21:02:10Z","status":"HALT"}`},
		{"bad timestamp", `{"symbol":"EUR_JPY","ask":"1","bid":"1","timestamp":"yesterday","status":"OPEN"}`},
		{"price is a number", `{"symbol":"EUR_JPY","ask":1,"bid":1,"timestamp":"2023-10-30T21:02:10Z","status":"OPEN"}`},
		{"row is not an object", `"EUR_JPY"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clk := newFakeClock()
			srv := newFakeServer(t, clk, step{body: `{"status":0,"data":[` + tt.extra + `,` + usd + `]}`})
			c, _ := testClient(t, srv, clk, Config{})

			got, err := c.Ticker(context.Background(), "USD_JPY")
			if err != nil || got.Bid != 149.808 || got.Ask != 149.812 {
				t.Fatalf("Ticker = %+v, %v; want the USD_JPY quote", got, err)
			}
		})
	}
}

func TestTickersConfiguredPairMissingOrInvalidStillErrors(t *testing.T) {
	const eur = `{"symbol":"EUR_JPY","ask":"158.771","bid":"158.759","timestamp":"2023-10-30T21:02:10Z","status":"OPEN"}`
	tests := []struct{ name, body string }{
		{"configured pair missing", `{"status":0,"data":[` + eur + `]}`},
		{"configured pair with zero price", `{"status":0,"data":[` + eur + `,{"symbol":"USD_JPY","ask":"0","bid":"149.8","timestamp":"2023-10-30T21:02:11Z","status":"OPEN"}]}`},
		{"configured pair with unknown status", `{"status":0,"data":[` + eur + `,{"symbol":"USD_JPY","ask":"149.9","bid":"149.8","timestamp":"2023-10-30T21:02:11Z","status":"HALT"}]}`},
		{"configured pair with bad timestamp", `{"status":0,"data":[` + eur + `,{"symbol":"USD_JPY","ask":"149.9","bid":"149.8","timestamp":"x","status":"OPEN"}]}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clk := newFakeClock()
			srv := newFakeServer(t, clk, step{body: tt.body})
			c, _ := testClient(t, srv, clk, Config{})

			_, err := c.Tickers(context.Background())
			if !errors.Is(err, ErrInvalidResponse) || len(srv.requests()) != 1 {
				t.Fatalf("err = %v, calls = %d; want ErrInvalidResponse after 1 call", err, len(srv.requests()))
			}
		})
	}
}

// Two rows for one configured pair are ambiguous: reject instead of silently
// keeping the last. Repeats and junk among unconfigured pairs stay ignored.
func TestTickerDuplicateRowForConfiguredPairIsInvalid(t *testing.T) {
	const usd = `{"symbol":"USD_JPY","ask":"149.812","bid":"149.808","timestamp":"2023-10-30T21:02:11Z","status":"OPEN"}`
	const usd2 = `{"symbol":"USD_JPY","ask":"150.012","bid":"150.008","timestamp":"2023-10-30T21:02:12Z","status":"OPEN"}`
	const junkEUR = `{"symbol":"EUR_JPY","ask":"0","bid":"0","timestamp":"x","status":"HALT"}`
	clk := newFakeClock()
	srv := newFakeServer(t, clk, step{body: `{"status":0,"data":[` + usd + `,` + usd2 + `]}`})
	c, _ := testClient(t, srv, clk, Config{})

	_, err := c.Ticker(context.Background(), "USD_JPY")
	if !errors.Is(err, ErrInvalidResponse) || errors.Is(err, ErrRetriesExhausted) || len(srv.requests()) != 1 {
		t.Fatalf("err = %v, calls = %d; want non-retried ErrInvalidResponse", err, len(srv.requests()))
	}

	srv = newFakeServer(t, clk, step{body: `{"status":0,"data":[` + junkEUR + `,` + usd + `,` + junkEUR + `]}`})
	c, _ = testClient(t, srv, clk, Config{})
	got, err := c.Ticker(context.Background(), "USD_JPY")
	if err != nil || got.Bid != 149.808 {
		t.Fatalf("Ticker = %+v, %v; repeated unconfigured rows must be ignored", got, err)
	}
}
