package gmo

import (
	"errors"
	"net/http"
	"slices"
	"testing"
	"time"
)

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, time.January, 5, 9, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		in   string
		want time.Duration
	}{
		{"empty", "", 0},
		{"delta seconds", "7", 7 * time.Second},
		{"delta seconds with spaces", " 7 ", 7 * time.Second},
		{"zero", "0", 0},
		{"negative", "-3", 0},
		{"fraction is not valid", "1.5", 0},
		{"garbage", "soon", 0},
		{"at the cap", "60", MaxRetryAfter},
		{"above the cap", "3600", MaxRetryAfter},
		{"overflowing number", "99999999999999999999999", MaxRetryAfter},
		{"http date in the future", now.Add(20 * time.Second).Format(http.TimeFormat), 20 * time.Second},
		{"http date beyond the cap", now.Add(24 * time.Hour).Format(http.TimeFormat), MaxRetryAfter},
		{"http date in the past", now.Add(-time.Minute).Format(http.TimeFormat), 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseRetryAfter(tt.in, now); got != tt.want {
				t.Fatalf("parseRetryAfter(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

// The wait before a retry is max(backoff, Retry-After): a Retry-After longer
// than the backoff lengthens it, a shorter or missing one never shortens it.
func TestRetryAfterLengthensButNeverShortensTheBackoff(t *testing.T) {
	clk := newFakeClock()
	httpDate := clk.Now().Add(15 * time.Second).Format(http.TimeFormat)
	tests := []struct {
		name      string
		steps     []step
		wantSleep []time.Duration
	}{
		{"delta-seconds longer than backoff", []step{{status: 429, retryAfter: "10"}, {status: 429, retryAfter: "10"}},
			[]time.Duration{10 * time.Second, 10 * time.Second}},
		{"http-date longer than backoff", []step{{status: 503, retryAfter: httpDate}, {status: 503}},
			[]time.Duration{15 * time.Second, 2 * time.Second}},
		{"shorter than backoff keeps the backoff", []step{{status: 429, retryAfter: "0"}, {status: 429, retryAfter: "1"}},
			[]time.Duration{time.Second, 2 * time.Second}},
		{"http-date in the past keeps the backoff", []step{{status: 503, retryAfter: clk.Now().Add(-time.Hour).Format(http.TimeFormat)}, {status: 503}},
			[]time.Duration{time.Second, 2 * time.Second}},
		{"malformed keeps the backoff", []step{{status: 503, retryAfter: "later"}, {status: 503, retryAfter: "-5"}},
			[]time.Duration{time.Second, 2 * time.Second}},
		{"huge value is capped", []step{{status: 429, retryAfter: "86400"}, {status: 429}},
			[]time.Duration{MaxRetryAfter, 2 * time.Second}},
		{"on a successful retry too", []step{{status: 429, retryAfter: "5"}, {status: 200, retryAfter: "99"}},
			[]time.Duration{5 * time.Second}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clk := newFakeClock()
			steps := slices.Clone(tt.steps)
			if steps[len(steps)-1].status == 200 {
				steps[len(steps)-1].fixture = "ticker.json"
			} else {
				steps = append(steps, step{fixture: "ticker.json"})
			}
			srv := newFakeServer(t, clk, steps...)
			c, _ := testClient(t, srv, clk, Config{})

			okTicker(t, c)

			if got := clk.recordedSleeps(); !slices.Equal(got, tt.wantSleep) {
				t.Fatalf("sleeps = %v, want %v", got, tt.wantSleep)
			}
		})
	}
}

// Property: for any Retry-After the sleep is >= the backoff alone, and equals
// max(backoff, capped Retry-After).
func TestRetryWaitIsNeverShorterThanBackoff(t *testing.T) {
	for _, backoff := range []time.Duration{time.Millisecond, time.Second, 30 * time.Second} {
		for _, ra := range []time.Duration{0, time.Millisecond, time.Second, 29 * time.Second, MaxRetryAfter} {
			got := retryWait(backoff, &APIError{HTTPStatus: 429, RetryAfter: ra})
			if got < backoff || got != max(backoff, ra) {
				t.Fatalf("retryWait(%v, RetryAfter %v) = %v", backoff, ra, got)
			}
		}
		if got := retryWait(backoff, &transportError{errors.New("x")}); got != backoff {
			t.Fatalf("transport error wait = %v, want backoff %v", got, backoff)
		}
	}
}
