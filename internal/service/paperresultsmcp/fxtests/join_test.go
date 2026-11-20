package fxtests

import (
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/fxdiag"
)

// AtOrBefore returns, for each time, the latest minute with both sides at or
// before it and within the staleness bound; otherwise nil (unmatched).
func TestAtOrBeforeMatchesTheLatestCandleWithinTheStaleness(t *testing.T) {
	_, w := newServer(t)
	candle(t, w, "USD_JPY", "2026-10-12", 0, "")     // 06:00 JST = 2026-10-11 21:00Z
	candle(t, w, "USD_JPY", "2026-10-12", 1, "")     // 21:01
	candle(t, w, "USD_JPY", "2026-10-12", 2, "bid")  // 21:02 BID only: skipped
	candle(t, w, "USD_JPY", "2026-10-12", 10, "ask") // 21:10 ASK only: skipped
	candle(t, w, "USD_JPY", "2026-10-12", 30, "")    // 21:30
	candle(t, w, "EUR_USD", "2026-10-12", 5, "")     // another pair never matches

	base := day("2026-10-12")
	at := func(min int, sec int) time.Time {
		return base.Add(time.Duration(min)*time.Minute + time.Duration(sec)*time.Second)
	}
	times := []time.Time{
		at(0, 0),                     // exactly at a candle's open
		at(1, 59),                    // inside minute 1: the minute that has opened
		at(3, 0),                     // minute 2 is BID only: skipped, so minute 1 (2 minutes old)
		at(6, 0),                     // minute 1 is exactly 5 minutes old: still matched
		at(6, 1),                     // 5 minutes 1 second old: unmatched
		at(-1, 0),                    // before the first candle
		at(29, 0),                    // minute 1 is 28 minutes old, minute 10 is ASK only: unmatched
		at(31, 30),                   // minute 30
		at(1, 59).Add(time.Hour * 0), // a repeat of an earlier time (the same answer)
	}
	got, err := fxdiag.AtOrBefore(t.Context(), w, "USD_JPY", times, fxdiag.DefaultStaleness)
	if err != nil {
		t.Fatal(err)
	}
	wantMinute := []int{0, 1, 1, 1, -1, -1, -1, 30, 1}
	for i, m := range wantMinute {
		switch {
		case m < 0 && got[i] != nil:
			t.Errorf("time %d (%v) matched %v, want unmatched", i, times[i], got[i].OpenTime)
		case m >= 0 && (got[i] == nil || !got[i].OpenTime.Equal(base.Add(time.Duration(m)*time.Minute))):
			t.Errorf("time %d (%v) = %+v, want the minute %d candle", i, times[i], got[i], m)
		}
	}
	if q := got[0]; q == nil || q.BidOpen != 150 || q.AskOpen != 150 || q.MidOpen() != 150 {
		t.Errorf("quote = %+v", q)
	}
	// A longer bound matches an older candle; the answer follows the argument.
	got, err = fxdiag.AtOrBefore(t.Context(), w, "USD_JPY", []time.Time{at(29, 0)}, 30*time.Minute)
	if err != nil || got[0] == nil || !got[0].OpenTime.Equal(at(1, 0)) {
		t.Errorf("30-minute staleness = %+v, %v; want minute 1", got[0], err)
	}
	// Nothing for no times, and no candle of an unknown pair.
	if got, err = fxdiag.AtOrBefore(t.Context(), w, "USD_JPY", nil, 0); err != nil || len(got) != 0 {
		t.Errorf("no times = %v, %v", got, err)
	}
	if got, err = fxdiag.AtOrBefore(t.Context(), w, "GBP_JPY", []time.Time{at(1, 0)}, 0); err != nil || got[0] != nil {
		t.Errorf("unknown pair = %v, %v", got, err)
	}
}
