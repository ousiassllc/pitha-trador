package jquantstests

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/jquantssettings"
	jqservice "github.com/ousiassllc/pitha-trador/internal/service/marketdata/jquants"
)

// With no API key the client sends no request at all, whichever read is asked:
// the transport behind it fails the test's count if it is ever reached.
func TestClient_NoKey_SendsNoRequest(t *testing.T) {
	transport := &countingTransport{}
	client := jqservice.NewClient(jqservice.Config{HTTP: &http.Client{Transport: transport}, MinInterval: -1})
	ctx := context.Background()

	if _, err := client.Symbols(ctx, backfillDay); !errors.Is(err, jqservice.ErrNoKey) {
		t.Errorf("Symbols err = %v, want ErrNoKey", err)
	}
	if _, err := client.DailyBars(ctx, backfillDay); !errors.Is(err, jqservice.ErrNoKey) {
		t.Errorf("DailyBars err = %v, want ErrNoKey", err)
	}
	if _, err := client.MinuteBars(ctx, backfillDay, "7203"); !errors.Is(err, jqservice.ErrNoKey) {
		t.Errorf("MinuteBars err = %v, want ErrNoKey", err)
	}
	if client.HasKey() {
		t.Error("HasKey = true for an empty key")
	}
	if n := transport.count(); n != 0 {
		t.Fatalf("%d requests reached the transport with no key, want 0", n)
	}
}

// Whatever else is set, the backfiller sends nothing while the switch is off, the
// key is missing or no endpoint is subscribed; it also writes nothing (no ledger row).
func TestBackfiller_Inert_WhenOffOrNoKeyOrNoPlan(t *testing.T) {
	enabled := settingsFor(true, true, 100)
	off := settingsFor(true, true, 100)
	off[jquantssettings.KeyEnabled] = "0"
	damaged := settingsFor(true, true, 100)
	damaged[jquantssettings.KeyEnabled] = `"yes"` // damaged: counts as off
	cases := map[string]struct {
		key      string
		settings mapStore
		skipped  string
	}{
		"switch off":        {testKey, off, "disabled"},
		"nothing stored":    {testKey, mapStore{}, "disabled"},
		"damaged switch":    {testKey, damaged, "disabled"},
		"no key":            {"", enabled, "no_key"},
		"no endpoint plans": {testKey, settingsFor(false, false, 100), "no_plan"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			r := newRig(t, c.key, c.settings)
			res, err := r.backfiller(0).Run(context.Background(), jqservice.Request{})
			if err != nil || res.Skipped != c.skipped {
				t.Fatalf("Run = (%+v, %v), want skipped %q", res, err, c.skipped)
			}
			if n := r.server.count(); n != 0 {
				t.Fatalf("%d requests were sent, want 0", n)
			}
			for _, table := range []string{"research_jquants_fetch_log", "research_jquants_coverage", "research_jquants_daily_bars"} {
				if n := r.count(t, table); n != 0 {
					t.Errorf("%s has %d rows after an inert run", table, n)
				}
			}
		})
	}
}

// An endpoint the plan flags do not list is never called, and the other is.
func TestBackfiller_UnsubscribedEndpointIsNeverCalled(t *testing.T) {
	r := newRig(t, testKey, settingsFor(true, false, 100))
	r.seedSnapshot(t, "2026-11-16T05:00:00.000000000Z", 3000, 2999, 3001)
	if _, err := r.backfiller(0).Run(context.Background(), jqservice.Request{}); err != nil {
		t.Fatal(err)
	}
	if n := r.server.countPath("bars/minute"); n != 0 {
		t.Errorf("the minute endpoint was called %d times without a minute plan", n)
	}
	if r.server.countPath("bars/daily") == 0 {
		t.Error("the subscribed daily endpoint was not called")
	}

	r = newRig(t, testKey, settingsFor(false, true, 100))
	r.seedSnapshot(t, "2026-11-16T05:00:00.000000000Z", 3000, 2999, 3001)
	if _, err := r.backfiller(0).Run(context.Background(), jqservice.Request{}); err != nil {
		t.Fatal(err)
	}
	if n := r.server.countPath("bars/daily"); n != 0 {
		t.Errorf("the daily endpoint was called %d times without a daily plan", n)
	}
	if r.server.countPath("bars/minute") == 0 {
		t.Error("the subscribed minute endpoint was not called")
	}
}
