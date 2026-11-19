package researchquotes

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/tachibanasource"
	"github.com/ousiassllc/pitha-trador/internal/service/broker"
)

// Only subscribed symbols are recorded, and a value the feed kept from an
// earlier JST day (EVENT down overnight) is no value today: it is not recorded
// as a price, the feed's rate-limited REST refill is asked instead.
func TestOnlySubscribedSymbolsAndTodaysValuesAreRecorded(t *testing.T) {
	r := newRig(t)
	r.kabu = []string{"7203", "6758"}
	r.feed.put("7203", 3000, thursday(9, 0, 0).AddDate(0, 0, -1), false) // yesterday's value
	r.feed.put("6758", 1500, thursday(9, 29, 50), false)
	r.feed.latest["7203"] = broker.Quote{Symbol: "7203", Price: 3010}
	r.tick(thursday(9, 30, 5))
	if got := r.feed.latestCalls; len(got) != 1 || got[0] != "7203" {
		t.Fatalf("Latest calls = %v, want only the symbol whose value is from yesterday", got)
	}
	if r.rows() != 2 {
		t.Fatalf("rows = %d, want 2", r.rows())
	}

	// 9984 joins kabu's list inside the hold-back window: it is planned but not
	// subscribed, so it is neither recorded nor asked for over REST.
	r.feed.put("7203", 3010, thursday(9, 30, 5), true) // what the real feed merges from the REST refill
	r.kabu = []string{"7203", "6758", "9984"}
	r.feed.put("9984", 8000, thursday(9, 35, 0), false)
	before := len(r.feed.latestCalls)
	r.tick(thursday(9, 31, 5))
	if r.rows() != 4 || len(r.feed.latestCalls) != before {
		t.Fatalf("rows=%d latest=%v, want 9984 not recorded before it is subscribed", r.rows(), r.feed.latestCalls)
	}
}

// A failing daily screen does not stop kabu's symbols from being recorded, and
// is not retried every minute.
func TestFailingDailyScreenKeepsRecordingKabuAndBacksOff(t *testing.T) {
	r := newRig(t)
	r.set.Research.QuotesExtraSource = tachibanasource.ResearchExtraDailyScreen
	calls := 0
	r.rec.cfg.ScreenSymbols = func(context.Context, tachibanasource.TachibanaSourceSettings, time.Time) ([]string, error) {
		calls++
		return nil, errors.New("bars unreadable")
	}
	r.kabu = []string{"7203"}
	r.feed.put("7203", 3000, thursday(9, 29, 50), false)
	for i := 0; i < 5; i++ {
		r.tick(thursday(9, 30+i, 5))
	}
	if r.rows() != 5 {
		t.Fatalf("rows = %d, want kabu's symbol recorded every minute", r.rows())
	}
	if calls != 1 {
		t.Fatalf("daily screen read %d times in 5 minutes, want 1 (backed off)", calls)
	}
	r.tick(thursday(9, 41, 5))
	if calls != 2 {
		t.Fatalf("daily screen read %d times after the back-off, want 2", calls)
	}
}
