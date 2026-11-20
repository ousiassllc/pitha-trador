package jquantstests

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	jqservice "github.com/ousiassllc/pitha-trador/internal/service/marketdata/jquants"
)

// The daily request cap is counted before a request is sent (pages included) and
// stops the calls at the cap: with a cap of 2, the master (1) and the first daily
// page (2) go out, the second page is refused without a request, and the unfinished
// day is not marked. The count is in the database, so a second run the same JST day
// sends nothing either; the next JST day continues.
func TestCap_StopsRequestsAtTheDailyCapAndResetsNextDay(t *testing.T) {
	r := newRig(t, testKey, settingsFor(true, false, 2))
	b := r.backfiller(0)

	res, err := b.Run(context.Background(), jqservice.Request{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.CapReached || r.server.count() != 2 {
		t.Fatalf("Run = %+v after %d requests, want the cap reached after exactly 2", res, r.server.count())
	}
	if got := r.count(t, "research_jquants_daily_bars"); got != 0 {
		t.Errorf("a day whose second page was refused stored %d bars, want none (all or nothing)", got)
	}
	if n, err := r.repo.RequestsOn(context.Background(), "2026-11-17"); err != nil || n != 2 {
		t.Errorf("ledger = %d (%v), want 2", n, err)
	}

	res, err = b.Run(context.Background(), jqservice.Request{})
	if err != nil || !res.CapReached || r.server.count() != 2 {
		t.Fatalf("second run = (%+v, %v) with %d requests in total, want the cap again and no new request", res, err, r.server.count())
	}

	// A restart does not reset the count: a new client over the same database is refused too.
	r.client = jqservice.NewClient(jqservice.Config{APIKey: testKey, BaseURL: r.server.URL + "/v2", HTTP: r.server.Client(),
		Ledger: jqservice.NewCapLedger(r.repo, r.capSource).WithClock(func() time.Time { return r.now }), MinInterval: -1})
	if _, err := r.client.DailyBars(context.Background(), backfillDay); !errors.Is(err, jqservice.ErrCapReached) || r.server.count() != 2 {
		t.Fatalf("DailyBars after a restart = %v with %d requests, want ErrCapReached and no request", err, r.server.count())
	}

	// The next JST day has a fresh count.
	r.now = r.now.AddDate(0, 0, 1)
	r.settings["jquants.daily_request_cap"] = "10"
	r.client = jqservice.NewClient(jqservice.Config{APIKey: testKey, BaseURL: r.server.URL + "/v2", HTTP: r.server.Client(),
		Ledger: jqservice.NewCapLedger(r.repo, r.capSource).WithClock(func() time.Time { return r.now }), MinInterval: -1})
	if _, err := r.client.DailyBars(context.Background(), backfillDay); err != nil {
		t.Fatalf("DailyBars the next day: %v", err)
	}
	if r.server.count() != 4 {
		t.Errorf("requests = %d, want 4 (2 pages the next day)", r.server.count())
	}
}

// A request is counted even when it fails, so failing requests cannot exceed the cap either.
func TestCap_CountsFailedRequests(t *testing.T) {
	r := newRig(t, testKey, settingsFor(true, false, 3))
	r.server.status = http.StatusInternalServerError
	for range 5 {
		_, _ = r.client.DailyBars(context.Background(), backfillDay)
	}
	if n := r.server.count(); n != 3 {
		t.Fatalf("%d requests were sent with a cap of 3", n)
	}
}

// 401/403 and 429 end the run with a fixed error (no response body, no key), the
// fixed code is recorded in the ledger, and nothing is marked or stored.
func TestFailures_AreFixedCodesWithoutBodyOrKey(t *testing.T) {
	cases := map[string]struct {
		status int
		want   error
		code   string
	}{
		"unauthorized": {http.StatusUnauthorized, jqservice.ErrAuth, "http_401"},
		"forbidden":    {http.StatusForbidden, jqservice.ErrAuth, "http_403"},
		"rate limited": {http.StatusTooManyRequests, jqservice.ErrRateLimited, "http_429"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			r := newRig(t, testKey, settingsFor(true, false, 100))
			r.server.status = c.status
			_, err := r.backfiller(0).Run(context.Background(), jqservice.Request{})
			if !errors.Is(err, c.want) {
				t.Fatalf("Run err = %v, want %v", err, c.want)
			}
			if strings.Contains(err.Error(), testKey) || strings.Contains(err.Error(), "must never be echoed") {
				t.Errorf("the error leaks a key or the body: %v", err)
			}
			if r.server.count() != 1 {
				t.Errorf("%d requests, want the run to stop after the first", r.server.count())
			}
			var code string
			var failures int
			if err := r.db.QueryRow(`SELECT last_failure_code, failures FROM research_jquants_fetch_log`).Scan(&code, &failures); err != nil || code != c.code || failures != 1 {
				t.Errorf("ledger = (%q, %d, %v), want (%q, 1)", code, failures, err, c.code)
			}
			if n := r.count(t, "research_jquants_coverage"); n != 0 {
				t.Errorf("%d coverage marks after a failure", n)
			}
		})
	}
}
