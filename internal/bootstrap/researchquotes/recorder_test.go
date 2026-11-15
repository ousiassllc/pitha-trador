package researchquotes

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/tachibanasource"
	"github.com/ousiassllc/pitha-trador/internal/repository/researchstatus"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
	"github.com/ousiassllc/pitha-trador/internal/service/broker"
)

// Switch off: no Start (so no login, connection or request) and no SetWatch,
// whatever the time of day; the status says so.
func TestTickSwitchOffNeverStartsOrTouchesTheFeed(t *testing.T) {
	r := newRig(t)
	r.set.Research.QuotesEnabled = false
	r.kabu = []string{"7203"}
	for _, now := range []time.Time{thursday(8, 55, 5), thursday(9, 30, 5), thursday(12, 0, 5), thursday(15, 0, 5)} {
		r.tick(now)
	}
	if r.starts != 0 || len(r.feed.watches) != 0 || len(r.feed.latestCalls) != 0 || r.rows() != 0 {
		t.Fatalf("starts=%d watches=%v latest=%v rows=%d, want none of them while the switch is off", r.starts, r.feed.watches, r.feed.latestCalls, r.rows())
	}
	if state, _ := r.state(); state != researchstatus.StateDisabled {
		t.Errorf("state = %s, want disabled", state)
	}
}

func TestTickWaitsOutsideTheTradingDayWindowWithoutAcquiring(t *testing.T) {
	for _, tc := range []struct {
		name    string
		now     time.Time
		started bool
	}{
		{"before 08:50", thursday(8, 49, 5), false},
		{"at 08:50 (acquire, nothing recorded before the open)", thursday(8, 50, 5), true},
		{"at the 15:30 close", thursday(15, 30, 5), false},
		{"evening", thursday(20, 0, 5), false},
		{"Saturday", time.Date(2026, 10, 10, 10, 0, 5, 0, time.FixedZone("JST", 9*3600)), false},
		{"national holiday (体育の日 2026-10-12)", time.Date(2026, 10, 12, 10, 0, 5, 0, time.FixedZone("JST", 9*3600)), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t)
			r.kabu = []string{"7203"}
			r.feed.put("7203", 3000, tc.now, false)
			r.tick(tc.now)
			if (r.starts == 1) != tc.started {
				t.Errorf("Start called %d times, want started=%v", r.starts, tc.started)
			}
			state, _ := r.state()
			if tc.started {
				if state != researchstatus.StateOK || r.rows() != 0 {
					t.Errorf("state=%s rows=%d, want ok with no row before the open", state, r.rows())
				}
			} else if state != researchstatus.StateWaiting || len(r.feed.watches) != 0 {
				t.Errorf("state=%s watches=%v, want waiting and no subscription", state, r.feed.watches)
			}
		})
	}
}

func TestTickStartOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name      string
		err       error
		wantState string
		wantErr   bool
	}{
		{"credentials unset", fmt.Errorf("hub: %w", ErrNotConfigured), researchstatus.StateNotConfigured, false},
		{"broker is tachibana", ErrBrokerIsTachibana, researchstatus.StateBrokerIsTachibana, false},
		{"any other failure", errors.New("boom"), researchstatus.StateError, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t)
			r.kabu = []string{"7203"}
			r.startE = tc.err
			err := r.rec.Tick(context.Background(), thursday(9, 30, 5))
			if (err != nil) != tc.wantErr {
				t.Errorf("Tick error = %v, want error=%v", err, tc.wantErr)
			}
			if state, _ := r.state(); state != tc.wantState {
				t.Errorf("state = %s, want %s", state, tc.wantState)
			}
			if len(r.feed.watches) != 0 || r.rows() != 0 {
				t.Errorf("watches=%v rows=%d, want nothing without a feed", r.feed.watches, r.rows())
			}
			// Start is tried again next minute (the credentials may have been set).
			r.startE = nil
			r.tick(thursday(9, 31, 5))
			if r.starts != 2 {
				t.Errorf("Start called %d times, want a retry the next minute", r.starts)
			}
		})
	}
}

func TestTickRecordsTheMinuteOfEveryPlannedSymbolThatHasAPrice(t *testing.T) {
	r := newRig(t)
	r.kabu = []string{"1001", "1002", "1003", "1004"}
	now := thursday(9, 30, 37) // recorded at the 5th second... or later: the row stands for the minute
	minute := thursday(9, 30, 0)
	bid, ask := 2999.0, 3001.0
	r.feed.values["1001"] = broker.Quote{Symbol: "1001", Price: 3000, Bid: &bid, Ask: &ask, VWAP: 2990, Volume: 500, Turnover: 1_500_000, SpecialQuote: true}
	r.feed.updated["1001"] = thursday(9, 29, 58)
	r.feed.put("1002", 1500, thursday(9, 20, 0), true) // an older value from a REST refill is recorded as it is, via rest
	r.feed.latest["1003"] = broker.Quote{Symbol: "1003", Price: 800}
	r.feed.latestErr = broker.ErrPriceUnavailable // 1004: no value and the refill is not allowed now

	r.tick(now)

	if want := []string{"1003", "1004"}; !slices.Equal(r.feed.latestCalls, want) {
		t.Errorf("REST refill asked for %v, want only the symbols without any value %v", r.feed.latestCalls, want)
	}
	type row struct {
		ts, symbol, via, phase string
		price                  float64
		bid, vwap, observed    sql.NullString
		special                int
	}
	var got []row
	q, err := r.db.Query(`SELECT timestamp, symbol, via, session_phase, price, CAST(bid AS TEXT), CAST(vwap AS TEXT), observed_at, special_quote
		FROM research_quote_samples WHERE source = 'tachibana' ORDER BY symbol`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = q.Close() }()
	for q.Next() {
		var x row
		if err := q.Scan(&x.ts, &x.symbol, &x.via, &x.phase, &x.price, &x.bid, &x.vwap, &x.observed, &x.special); err != nil {
			t.Fatal(err)
		}
		got = append(got, x)
	}
	if err := q.Err(); err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("%d rows, want 3 (1004 has no price and gets no row): %+v", len(got), got)
	}
	ts := sqlutil.FormatTime(minute)
	if a := got[0]; a.symbol != "1001" || a.ts != ts || a.via != "event" || a.phase != "morning" || a.price != 3000 ||
		a.bid.String != "2999" || a.vwap.String != "2990" || a.special != 1 || a.observed.String != sqlutil.FormatTime(thursday(9, 29, 58)) {
		t.Errorf("1001 row = %+v, want the minute %s, event, morning, price 3000, bid 2999, vwap 2990, special, observed 09:29:58", a, ts)
	}
	if b := got[1]; b.symbol != "1002" || b.via != "rest" || b.ts != ts {
		t.Errorf("1002 row = %+v, want via rest", b)
	}
	if c := got[2]; c.symbol != "1003" || c.via != "rest" || c.price != 800 || c.observed.Valid || c.vwap.Valid || c.bid.Valid {
		t.Errorf("1003 row = %+v, want the REST answer with no observed_at, vwap or bid", c)
	}
	if state, detail := r.state(); state != researchstatus.StateRunning || detail == "" {
		t.Errorf("state = %s (%q), want running with a detail", state, detail)
	}

	// The same minute again (another second, a restart) adds nothing.
	r.tick(thursday(9, 30, 50))
	if n := r.rows(); n != 3 {
		t.Errorf("%d rows after recording the same minute twice, want 3", n)
	}
	// The next minute is a new row for each symbol that still has a price.
	r.tick(thursday(9, 31, 5))
	if n := r.rows(); n != 6 {
		t.Errorf("%d rows after the next minute, want 6", n)
	}
}

func TestTickRecordsNothingOutsideTheSessionButKeepsThePlanSubscribed(t *testing.T) {
	r := newRig(t)
	r.kabu = []string{"7203"}
	r.feed.put("7203", 3000, thursday(11, 29, 0), false)
	r.tick(thursday(12, 0, 5)) // lunch break: inside the 08:50-15:30 window, market closed
	if r.rows() != 0 || len(r.feed.latestCalls) != 0 {
		t.Errorf("rows=%d latest=%v, want nothing recorded or requested at lunch", r.rows(), r.feed.latestCalls)
	}
	if len(r.feed.watches) != 1 {
		t.Errorf("watches = %v, want the symbols subscribed ahead of the session", r.feed.watches)
	}
	r.tick(thursday(12, 36, 5))
	if r.rows() != 1 {
		t.Errorf("rows = %d, want 1 once the afternoon session runs", r.rows())
	}
}

func TestTickSetWatchOnlyWhenThePlannedSetChanges(t *testing.T) {
	r := newRig(t)
	r.set = settings("9984")
	r.kabu = []string{"7203", "6758"}
	for i := 0; i < 3; i++ {
		r.tick(thursday(9, 30+i, 5))
	}
	if len(r.feed.watches) != 1 || !slices.Equal(r.feed.watches[0], []string{"7203", "6758", "9984"}) {
		t.Fatalf("watches = %v, want one call with kabu's symbols first, then the extra", r.feed.watches)
	}
	// kabu drops 6758 and adds 8306: only the addition changes the subscription.
	r.kabu = []string{"7203", "8306"}
	r.tick(thursday(9, 33, 5))
	r.tick(thursday(9, 34, 5))
	if len(r.feed.watches) != 2 || !slices.Equal(r.feed.watches[1], []string{"7203", "6758", "9984", "8306"}) {
		t.Fatalf("watches = %v, want a second call with the sticky list plus 8306", r.feed.watches)
	}
	// A shrinking source changes nothing.
	r.kabu = nil
	r.tick(thursday(9, 35, 5))
	if len(r.feed.watches) != 2 {
		t.Errorf("watches = %v, want no call when only symbols went away", r.feed.watches)
	}
}

func TestTickCapsThePlanAtMaxSymbolsAndTheEventCap(t *testing.T) {
	for _, tc := range []struct{ max, want int }{{120, 120}, {100, 100}, {50, 50}, {1, 1}} {
		r := newRig(t)
		r.set = settings(symbolsN("X", 100)...)
		r.set.Research.QuotesMaxSymbols = tc.max
		r.kabu = symbolsN("K", 50)
		r.tick(thursday(9, 30, 5))
		if len(r.feed.watches) != 1 || len(r.feed.watches[0]) != tc.want {
			t.Fatalf("max_symbols %d: watches %d symbols, want %d", tc.max, len(r.feed.watches[0]), tc.want)
		}
		if tc.max >= 50 && !slices.Equal(r.feed.watches[0][:50], r.kabu) {
			t.Errorf("max_symbols %d: kabu's symbols are not first", tc.max)
		}
	}
	// A larger value than the broker's EVENT cap (not reachable through the
	// validated setting) is still held to 120.
	r := newRig(t)
	r.set = settings(symbolsN("X", 100)...)
	r.set.Research.QuotesMaxSymbols = 500
	r.kabu = symbolsN("K", 50)
	r.tick(thursday(9, 30, 5))
	if n := len(r.feed.watches[0]); n != 120 {
		t.Errorf("a max_symbols of 500 planned %d symbols, want 120", n)
	}
}

func TestTickDailyScreenExtrasComeAfterKabuAndAreComputedOncePerDay(t *testing.T) {
	r := newRig(t)
	r.set = settings("0000") // the manual list is ignored under daily_screen
	r.set.Research.QuotesExtraSource = tachibanasource.ResearchExtraDailyScreen
	r.kabu = []string{"7203"}
	calls := 0
	r.rec.cfg.ScreenSymbols = func(context.Context, tachibanasource.TachibanaSourceSettings, time.Time) ([]string, error) {
		calls++
		return []string{"6758", "7203", "9984"}, nil
	}
	for i := 0; i < 3; i++ {
		r.tick(thursday(9, 30+i, 5))
	}
	if calls != 1 {
		t.Errorf("the screen ran %d times in a day, want 1", calls)
	}
	if want := []string{"7203", "6758", "9984"}; len(r.feed.watches) != 1 || !slices.Equal(r.feed.watches[0], want) {
		t.Errorf("watches = %v, want %v (kabu first, the screen's picks after, deduplicated, manual list unused)", r.feed.watches, want)
	}
	r.tick(time.Date(2026, 10, 9, 9, 30, 5, 0, thursday(0, 0, 0).Location()))
	if calls != 2 {
		t.Errorf("the screen ran %d times over two days, want 2", calls)
	}
}

func TestTickWithoutASessionReportsAnErrorInsteadOfRows(t *testing.T) {
	r := newRig(t)
	r.kabu = []string{"7203", "6758"}
	r.feed.latestErr = broker.ErrNoSession
	r.tick(thursday(9, 30, 5))
	state, detail := r.state()
	if state != researchstatus.StateError || detail == "" || r.rows() != 0 {
		t.Errorf("state=%s detail=%q rows=%d, want error, a reason and no row", state, detail, r.rows())
	}
}

func TestTickSettingsErrorIsReturnedWithoutAcquiring(t *testing.T) {
	r := newRig(t)
	r.rec.cfg.Settings = func(context.Context) (tachibanasource.TachibanaSourceSettings, error) {
		return tachibanasource.TachibanaSourceSettings{}, errors.New("db down")
	}
	if err := r.rec.Tick(context.Background(), thursday(9, 30, 5)); err == nil || r.starts != 0 {
		t.Errorf("err=%v starts=%d, want an error and no Start", err, r.starts)
	}
}
