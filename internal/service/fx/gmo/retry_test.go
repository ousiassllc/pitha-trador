package gmo

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"sync/atomic"
	"testing"
	"time"
)

func countLogs(recs []logRecord, level, msg string) int {
	n := 0
	for _, r := range recs {
		if r.Level == level && r.Msg == msg {
			n++
		}
	}
	return n
}

func okTicker(t *testing.T, c *Client) {
	t.Helper()
	got, err := c.Ticker(context.Background(), "USD_JPY")
	if err != nil {
		t.Fatalf("Ticker: %v", err)
	}
	if got.Bid != 149.808 {
		t.Fatalf("Ticker = %+v", got)
	}
}

func TestRetryTransientFailuresThenSuccess(t *testing.T) {
	tests := []struct {
		name  string
		fails []step
	}{
		{"http 503 then 502 html", []step{{status: 503, body: ""}, {status: 502, body: "<html>bad gateway</html>"}}},
		{"http 500", []step{{status: 500, body: `{"status":1}`}, {status: 500, body: `{"status":1}`}}},
		{"http 429", []step{{status: 429, body: ""}, {status: 429, body: ""}}},
		{"api rate limit ERR-5003 (http 200)", []step{{fixture: "error_rate_limit.json"}, {fixture: "error_rate_limit.json"}}},
		{"api maintenance ERR-5201 status 5 (http 200)", []step{{fixture: "error_maintenance.json"}, {fixture: "error_maintenance.json"}}},
		{"mixed", []step{{status: 503}, {fixture: "error_rate_limit.json"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clk := newFakeClock()
			srv := newFakeServer(t, clk, append(slices.Clone(tt.fails), step{fixture: "ticker.json"})...)
			c, logs := testClient(t, srv, clk, Config{})

			okTicker(t, c)

			if n := len(srv.requests()); n != 3 {
				t.Fatalf("%d calls, want 3", n)
			}
			if got, want := clk.recordedSleeps(), []time.Duration{time.Second, 2 * time.Second}; !slices.Equal(got, want) {
				t.Fatalf("sleeps = %v, want exponential backoff %v", got, want)
			}
			recs := logs.records(t)
			if n := countLogs(recs, "WARN", "gmo: request failed, retrying"); n != 2 {
				t.Fatalf("%d retry warnings, want 2 (%+v)", n, recs)
			}
			if countLogs(recs, "ERROR", "gmo: giving up after repeated failures") != 0 {
				t.Fatal("must not give up when a retry succeeds")
			}
		})
	}
}

func TestRetryNetworkErrorThenSuccess(t *testing.T) {
	clk := newFakeClock()
	srv := newFakeServer(t, clk, step{fixture: "ticker.json"})
	var calls atomic.Int32
	flaky := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if calls.Add(1) <= 2 {
			return nil, errors.New("dial tcp: connection refused")
		}
		return http.DefaultTransport.RoundTrip(r)
	})
	c, _ := testClient(t, srv, clk, Config{}, WithHTTPClient(&http.Client{Transport: flaky}))

	okTicker(t, c)

	if calls.Load() != 3 || len(srv.requests()) != 1 {
		t.Fatalf("transport calls = %d, server calls = %d; want 3 and 1", calls.Load(), len(srv.requests()))
	}
	if got, want := clk.recordedSleeps(), []time.Duration{time.Second, 2 * time.Second}; !slices.Equal(got, want) {
		t.Fatalf("sleeps = %v, want %v", got, want)
	}
}

func TestRetryStopsAfterMaxAttemptsWithErrorAndLog(t *testing.T) {
	clk := newFakeClock()
	srv := newFakeServer(t, clk, step{status: 503}, step{status: 503}, step{status: 503})
	c, logs := testClient(t, srv, clk, Config{MaxAttempts: 3})

	_, err := c.Ticker(context.Background(), "USD_JPY")

	if !errors.Is(err, ErrRetriesExhausted) {
		t.Fatalf("err = %v, want ErrRetriesExhausted", err)
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.HTTPStatus != 503 {
		t.Fatalf("err = %v, want the last failure (http 503) wrapped", err)
	}
	if n := len(srv.requests()); n != 3 {
		t.Fatalf("%d calls, want exactly MaxAttempts=3", n)
	}
	// No sleep after the last failure: only 2 backoffs for 3 attempts.
	if got, want := clk.recordedSleeps(), []time.Duration{time.Second, 2 * time.Second}; !slices.Equal(got, want) {
		t.Fatalf("sleeps = %v, want %v", got, want)
	}
	recs := logs.records(t)
	if countLogs(recs, "ERROR", "gmo: giving up after repeated failures") != 1 {
		t.Fatalf("want one ERROR log when giving up, got %+v", recs)
	}
	if countLogs(recs, "WARN", "gmo: request failed, retrying") != 2 {
		t.Fatalf("want 2 retry warnings, got %+v", recs)
	}
}

func TestRetryDefaultAttemptsAndBackoffCap(t *testing.T) {
	tests := []struct {
		name      string
		cfg       Config
		wantCalls int
		wantSleep []time.Duration
	}{
		{"defaults", Config{}, DefaultMaxAttempts, []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second}},
		{"MaxBackoff caps the doubling", Config{MaxBackoff: 3 * time.Second}, DefaultMaxAttempts, []time.Duration{time.Second, 2 * time.Second, 3 * time.Second, 3 * time.Second}},
		{"custom InitialBackoff", Config{InitialBackoff: 5 * time.Second, MaxAttempts: 3}, 3, []time.Duration{5 * time.Second, 10 * time.Second}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clk := newFakeClock()
			steps := make([]step, tt.wantCalls)
			for i := range steps {
				steps[i] = step{status: 500}
			}
			srv := newFakeServer(t, clk, steps...)
			c, _ := testClient(t, srv, clk, tt.cfg)

			_, err := c.Ticker(context.Background(), "USD_JPY")
			if !errors.Is(err, ErrRetriesExhausted) {
				t.Fatalf("err = %v", err)
			}
			if n := len(srv.requests()); n != tt.wantCalls {
				t.Fatalf("%d calls, want %d", n, tt.wantCalls)
			}
			if got := clk.recordedSleeps(); !slices.Equal(got, tt.wantSleep) {
				t.Fatalf("sleeps = %v, want %v", got, tt.wantSleep)
			}
		})
	}
}

func TestNonRetryableErrorsAreReturnedAtOnce(t *testing.T) {
	tests := []struct {
		name       string
		step       step
		wantHTTP   int
		wantStatus int
		wantCode   string
	}{
		{"http 400 invalid parameter", step{status: 400, fixture: "error_invalid_param.json"}, 400, 1, "ERR-5207"},
		{"http 404 not found", step{status: 404, body: "not found"}, 404, 0, ""},
		{"http 403", step{status: 403, body: ""}, 403, 0, ""},
		{"api invalid symbol/interval/date (http 200)", step{fixture: "error_invalid_param.json"}, 200, 1, "ERR-5207"},
		{"api invalid parameter ERR-5106", step{body: `{"status":1,"messages":[{"message_code":"ERR-5106","message_string":"Invalid request parameter."}]}`}, 200, 1, "ERR-5106"},
		{"api unknown code", step{body: `{"status":1,"messages":[{"message_code":"ERR-9999","message_string":"?"}]}`}, 200, 1, "ERR-9999"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clk := newFakeClock()
			srv := newFakeServer(t, clk, tt.step)
			c, logs := testClient(t, srv, clk, Config{})

			_, err := c.Ticker(context.Background(), "USD_JPY")

			var apiErr *APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("err = %v, want *APIError", err)
			}
			if errors.Is(err, ErrRetriesExhausted) || apiErr.Retryable() {
				t.Fatalf("err = %v: must be permanent", err)
			}
			if apiErr.HTTPStatus != tt.wantHTTP || apiErr.Status != tt.wantStatus {
				t.Fatalf("APIError = %+v, want http %d status %d", apiErr, tt.wantHTTP, tt.wantStatus)
			}
			if tt.wantCode != "" && (len(apiErr.Messages) != 1 || apiErr.Messages[0].Code != tt.wantCode) {
				t.Fatalf("messages = %+v, want code %s", apiErr.Messages, tt.wantCode)
			}
			if n := len(srv.requests()); n != 1 {
				t.Fatalf("%d calls, want 1 (no retry)", n)
			}
			if s := clk.recordedSleeps(); len(s) != 0 {
				t.Fatalf("slept %v, want no backoff", s)
			}
			if countLogs(logs.records(t), "ERROR", "gmo: giving up after repeated failures") != 0 {
				t.Fatal("a permanent failure is not 'giving up after repeated failures'")
			}
		})
	}
}

func TestKlinesInvalidDateFromServerIsNotRetried(t *testing.T) {
	clk := newFakeClock()
	srv := newFakeServer(t, clk, step{fixture: "error_invalid_param.json"})
	c, _ := testClient(t, srv, clk, Config{})

	_, err := c.Klines(context.Background(), KlinesRequest{Symbol: "USD_JPY", PriceType: PriceBid, Interval: Interval1Min, At: monday})

	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Retryable() || len(srv.requests()) != 1 {
		t.Fatalf("err = %v, calls = %d; want one call, permanent *APIError", err, len(srv.requests()))
	}
}

func TestRetriesRespectTheCallRateLimit(t *testing.T) {
	clk := newFakeClock()
	srv := newFakeServer(t, clk, step{status: 503}, step{status: 503}, step{fixture: "ticker.json"})
	// A 1ms backoff alone would retry almost at once; the limiter must still
	// space the three HTTP calls 1s apart.
	c, _ := testClient(t, srv, clk, Config{InitialBackoff: time.Millisecond})

	start := clk.Now()
	okTicker(t, c)

	arr := srv.arrivals()
	if len(arr) != 3 {
		t.Fatalf("%d calls, want 3", len(arr))
	}
	for i := 1; i < len(arr); i++ {
		if gap := arr[i].Sub(arr[i-1]); gap < time.Second {
			t.Fatalf("calls %d and %d are %s apart, want >= 1s (arrivals offsets %v)", i, i+1, gap, arr[i].Sub(start))
		}
	}
}

func TestContextCancelStopsRetrying(t *testing.T) {
	clk := newFakeClock()
	srv := newFakeServer(t, clk)
	ctx, cancel := context.WithCancel(context.Background())
	var calls atomic.Int32
	cancelling := roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		cancel()
		return nil, context.Canceled
	})
	c, logs := testClient(t, srv, clk, Config{}, WithHTTPClient(&http.Client{Transport: cancelling}))

	_, err := c.Ticker(ctx, "USD_JPY")

	if !errors.Is(err, context.Canceled) || errors.Is(err, ErrRetriesExhausted) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if calls.Load() != 1 || len(clk.recordedSleeps()) != 0 {
		t.Fatalf("calls = %d, sleeps = %v; want 1 and none", calls.Load(), clk.recordedSleeps())
	}
	if countLogs(logs.records(t), "ERROR", "gmo: giving up after repeated failures") != 0 {
		t.Fatal("cancellation is not a give-up")
	}
}
